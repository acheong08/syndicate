package relay_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

// generateSelfSignedCert creates a self-signed certificate and returns tls.Certificate and raw DER bytes.
func generateSelfSignedCert() (tls.Certificate, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	return cert, derBytes, err
}

func TestDeviceIdIdentical(t *testing.T) {
	var clientSeenDevice protocol.DeviceID
	var serverSeenDevice protocol.DeviceID

	cert, _, err := generateSelfSignedCert()
	if err != nil {
		t.Error(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
	})
	if err != nil {
		t.Error(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Error(err)
		}
		tlsConn := conn.(*tls.Conn)
		if err := tlsConn.Handshake(); err != nil {
			t.Error(err)
		}
		state := tlsConn.ConnectionState()
		serverSeenDevice = protocol.NewDeviceID(state.PeerCertificates[0].Raw)
		conn.Close()
		close(done)
	}()

	clientCert, _, err := generateSelfSignedCert()
	if err != nil {
		t.Error(err)
	}
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		Certificates:       []tls.Certificate{clientCert},
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Error(err)
	}
	clientSeenDevice = protocol.NewDeviceID(clientCert.Certificate[0])
	conn.Close()
	<-done

	if !clientSeenDevice.Equals(serverSeenDevice) {
		t.Errorf("client and server seen certificate differs: %s - %s", clientSeenDevice.Short(), serverSeenDevice.Short())
	}
}
