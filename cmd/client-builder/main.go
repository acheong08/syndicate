package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/gob"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"syndicate/lib"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/rand"
)

var configFolder string

func init() {
	var err error
	configFolder, err = os.UserConfigDir()
	if err != nil {
		panic(err)
	}
	configFolder += "/syndicate"
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <client label>")
		return
	}
	clientLabel := os.Args[1]
	cert, key, _ := generateCertificate("syndicate", 182)
	// Save the certificate to certs/client.crt
	certFile, err := newFile("cmd/client/certs/client.crt")
	if err != nil {
		panic(err)
	}
	defer certFile.Close()
	pem.Encode(certFile, cert)
	// Save the key to certs/client.key
	keyFile, err := newFile("cmd/client/certs/client.key")
	if err != nil {
		panic(err)
	}
	defer keyFile.Close()
	pem.Encode(keyFile, key)
	clientCert, _ := tls.X509KeyPair(pem.EncodeToMemory(cert), pem.EncodeToMemory(key))
	deviceID := protocol.NewDeviceID(clientCert.Certificate[0])
	fmt.Println("clientID", deviceID.String())
	if _, err := os.Stat(configFolder); os.IsNotExist(err) {
		os.Mkdir(configFolder, 0755)
	}
	var clientList lib.ClientList
	if _, err := os.Stat(configFolder + "/clients.bin"); err == nil {
		// Read the client list from the file and decode with gob
		file, err := os.Open(configFolder + "/clients.bin")
		defer file.Close()
		if err == nil {
			decoder := gob.NewDecoder(file)
			_ = decoder.Decode(&clientList)
		}
	}
	serverCert, serverKey, err := generateCertificate("syndicate-server", 182)
	if err != nil {
		panic(err)
	}
	// Generate server device ID
	serverX509Cert, _ := tls.X509KeyPair(pem.EncodeToMemory(serverCert), pem.EncodeToMemory(serverKey))
	serverDeviceID := protocol.NewDeviceID(serverX509Cert.Certificate[0])
	fmt.Println("serverID", serverDeviceID.String())
	clientList = append(clientList, lib.ClientEntry{
		Label:      clientLabel,
		ClientID:   deviceID,
		ClientCert: clientCert.Certificate[0],
		ServerID:   serverDeviceID.String(),
		ServerCert: [][]byte{pem.EncodeToMemory(serverCert), pem.EncodeToMemory(serverKey)},
	})
	// Save the client list to the file
	file, err := newFile(configFolder + "/clients.bin")
	defer file.Close()
	if err != nil {
		panic(err)
	}
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(clientList)
	if err != nil {
		panic(err)
	}
	// Set CGO_ENABLED=0 to build the client without cgo
	os.Setenv("CGO_ENABLED", "0")
	// Compile the client by running `go build ./cmd/client`
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", fmt.Sprintf("-X main.serverID=%s -s -w", serverDeviceID.String()), "./cmd/client")
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", stdoutStderr)
}

func newFile(filepath string) (*os.File, error) {
	// Split the filepath into directory and filename
	dir := strings.Join(strings.Split(filepath, "/")[:len(strings.Split(filepath, "/"))-1], "/")
	// Check if the directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		// Create the directory
		err = os.Mkdir(dir, 0755)
		if err != nil {
			return nil, err
		}
	}
	// Save the data to the file
	file, err := os.Create(filepath)
	if err != nil {
		return nil, err
	}
	return file, nil
}

// generateCertificate generates a PEM formatted key pair and self-signed certificate in memory.
// Copied from https://github.com/syncthing/syncthing/blob/main/lib/tlsutil/tlsutil.go
func generateCertificate(commonName string, lifetimeDays int) (*pem.Block, *pem.Block, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	notBefore := time.Now().Truncate(24 * time.Hour)
	notAfter := notBefore.Add(time.Duration(lifetimeDays*24) * time.Hour)

	// NOTE: update lib/api.shouldRegenerateCertificate() appropriately if
	// you add or change attributes in here, especially DNSNames or
	// IPAddresses.
	template := x509.Certificate{
		SerialNumber: new(big.Int).SetUint64(rand.Uint64()),
		Subject: pkix.Name{
			CommonName:         commonName,
			Organization:       []string{"Syncthing"},
			OrganizationalUnit: []string{"Automatically Generated"},
		},
		DNSNames:              []string{commonName},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, priv.Public(), priv)
	if err != nil {
		return nil, nil, fmt.Errorf("create cert: %w", err)
	}

	certBlock := &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}
	keyBlock, err := pemBlockForKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("save key: %w", err)
	}

	return certBlock, keyBlock, nil
}

func pemBlockForKey(priv interface{}) (*pem.Block, error) {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}, nil
	case *ecdsa.PrivateKey:
		b, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			return nil, err
		}
		return &pem.Block{Type: "EC PRIVATE KEY", Bytes: b}, nil
	default:
		return nil, errors.New("unknown key type")
	}
}
