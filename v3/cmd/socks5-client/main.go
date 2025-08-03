package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/acheong08/syndicate/v3/syndicate"
	"github.com/acheong08/syndicate/v3/transport"
	"github.com/syncthing/syncthing/lib/protocol"
)

type Config struct {
	ListenAddr string `json:"listen_addr"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
}

// SOCKS5 constants
const (
	socks5Version     = 0x05
	noAuth            = 0x00
	connect           = 0x01
	ipv4              = 0x01
	domainName        = 0x03
	ipv6              = 0x04
	succeeded         = 0x00
	generalFailure    = 0x01
	connectionRefused = 0x05
)

func loadConfig(configPath string) (*Config, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var config Config
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	return &config, nil
}

func defaultConfig() *Config {
	return &Config{
		ListenAddr: ":1080",
		CertFile:   "",
		KeyFile:    "",
	}
}

func main() {
	var (
		configFile = flag.String("config", "", "Path to config file (JSON format)")
		listenAddr = flag.String("listen", ":1080", "SOCKS5 listen address")
		certFile   = flag.String("cert", "", "TLS certificate file")
		keyFile    = flag.String("key", "", "TLS key file")
	)
	flag.Parse()

	// Load configuration
	var config *Config
	var err error

	if *configFile != "" {
		config, err = loadConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	} else {
		config = defaultConfig()
		config.ListenAddr = *listenAddr
		config.CertFile = *certFile
		config.KeyFile = *keyFile
	}

	// Load or generate TLS certificate
	cert, err := loadOrGenerateCert(config.CertFile, config.KeyFile)
	if err != nil {
		log.Fatalf("Failed to load/generate certificate: %v", err)
	}

	// Derive device ID from certificate
	deviceID := protocol.NewDeviceID(cert.Certificate[0])
	log.Printf("Starting SOCKS5 client with device ID: %s", deviceID.String())
	log.Printf("Listening on: %s", config.ListenAddr)

	// Start pprof server for debugging
	go func() {
		log.Printf("Starting pprof server on :6060")
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	// Create syndicate client
	client, err := createSyndicateClient(cert, deviceID)
	if err != nil {
		log.Fatalf("Failed to create syndicate client: %v", err)
	}
	defer client.Close()

	// Start SOCKS5 proxy
	listener, err := net.Listen("tcp", config.ListenAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", config.ListenAddr, err)
	}
	defer listener.Close()

	log.Printf("SOCKS5 proxy listening on %s", config.ListenAddr)

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Printf("Received shutdown signal")
		cancel()
		listener.Close()
	}()

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				log.Printf("Shutting down...")
				return
			default:
				log.Printf("Accept error: %v", err)
				continue
			}
		}

		go handleSocks5Connection(ctx, conn, client)
	}
}

func loadOrGenerateCert(certFile, keyFile string) (tls.Certificate, error) {
	if certFile != "" && keyFile != "" {
		return tls.LoadX509KeyPair(certFile, keyFile)
	}

	// Generate a self-signed certificate
	log.Printf("Generating self-signed certificate...")

	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Syndicate SOCKS5 Client"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:    []string{"localhost"},
	}

	// Generate certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create certificate: %w", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  privateKey,
	}, nil
}

func createSyndicateClient(cert tls.Certificate, deviceID syndicate.DeviceID) (syndicate.Client, error) {
	// Create syndicate client
	client, err := syndicate.NewClient(syndicate.ClientConfig{
		Certificate:       cert,
		DeviceID:          deviceID,
		MaxConnections:    10,
		ConnectionTimeout: 30 * time.Second,
		Country:           "", // Auto-detect
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create syndicate client: %w", err)
	}

	return client, nil
}

func handleSocks5Connection(ctx context.Context, clientConn net.Conn, syndicateClient syndicate.Client) {
	defer clientConn.Close()

	// SOCKS5 authentication
	if err := handleSocks5Auth(clientConn); err != nil {
		log.Printf("SOCKS5 auth failed: %v", err)
		return
	}

	// SOCKS5 connect request
	targetAddr, err := handleSocks5Connect(clientConn)
	if err != nil {
		log.Printf("SOCKS5 connect failed: %v", err)
		return
	}

	log.Printf("SOCKS5 connection request to: %s", targetAddr)

	// Strip port
	targetAddrPort := strings.Split(targetAddr, ":")
	if len(targetAddrPort) == 2 {
		targetAddr = targetAddrPort[0]
	}

	// Check if target is a .syncthing domain
	if !strings.HasSuffix(targetAddr, ".syncthing") {
		// Not a .syncthing domain, reject the connection
		sendSocks5Response(clientConn, connectionRefused, "0.0.0.0", 0)
		log.Printf("Rejected non-.syncthing domain: %s", targetAddr)
		return
	}

	// Parse .syncthing domain to extract device ID
	endpoint, err := transport.NewRelayEndpoint(targetAddr)
	if err != nil {
		sendSocks5Response(clientConn, generalFailure, "0.0.0.0", 0)
		log.Printf("Failed to parse .syncthing address %s: %v", targetAddr, err)
		return
	}

	// Connect to target device via syndicate
	targetConn, err := syndicateClient.Connect(ctx, endpoint.DeviceID())
	if err != nil {
		sendSocks5Response(clientConn, generalFailure, "0.0.0.0", 0)
		log.Printf("Failed to connect to device %s: %v", endpoint.DeviceID().String(), err)
		return
	}
	defer targetConn.Close()

	// Send success response
	sendSocks5Response(clientConn, succeeded, "0.0.0.0", 0)
	log.Printf("Successfully connected to device: %s", endpoint.DeviceID().String())

	// Proxy data between client and target
	proxyData(clientConn, targetConn)
}

func handleSocks5Auth(conn net.Conn) error {
	// Read version and number of authentication methods
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("failed to read auth header: %w", err)
	}

	version := buf[0]
	nmethods := buf[1]

	if version != socks5Version {
		return fmt.Errorf("unsupported SOCKS version: %d", version)
	}

	// Read authentication methods
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("failed to read auth methods: %w", err)
	}

	// Check if no authentication is supported
	for _, method := range methods {
		if method == noAuth {
			// Send response: version + chosen method
			response := []byte{socks5Version, noAuth}
			if _, err := conn.Write(response); err != nil {
				return fmt.Errorf("failed to send auth response: %w", err)
			}
			return nil
		}
	}

	// No supported authentication method
	response := []byte{socks5Version, 0xFF}
	conn.Write(response)
	return fmt.Errorf("no supported authentication method")
}

func handleSocks5Connect(conn net.Conn) (string, error) {
	// Read connect request header
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", fmt.Errorf("failed to read connect header: %w", err)
	}

	version := buf[0]
	cmd := buf[1]
	// buf[2] is reserved
	addrType := buf[3]

	if version != socks5Version {
		return "", fmt.Errorf("unsupported SOCKS version: %d", version)
	}

	if cmd != connect {
		return "", fmt.Errorf("unsupported command: %d", cmd)
	}

	// Read target address
	var addr string
	var port uint16

	switch addrType {
	case ipv4:
		ipBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, ipBuf); err != nil {
			return "", fmt.Errorf("failed to read IPv4 address: %w", err)
		}
		addr = net.IP(ipBuf).String()

	case domainName:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", fmt.Errorf("failed to read domain length: %w", err)
		}

		domainLen := lenBuf[0]
		domainBuf := make([]byte, domainLen)
		if _, err := io.ReadFull(conn, domainBuf); err != nil {
			return "", fmt.Errorf("failed to read domain name: %w", err)
		}
		addr = string(domainBuf)

	case ipv6:
		ipBuf := make([]byte, 16)
		if _, err := io.ReadFull(conn, ipBuf); err != nil {
			return "", fmt.Errorf("failed to read IPv6 address: %w", err)
		}
		addr = net.IP(ipBuf).String()

	default:
		return "", fmt.Errorf("unsupported address type: %d", addrType)
	}

	// Read port
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", fmt.Errorf("failed to read port: %w", err)
	}
	port = binary.BigEndian.Uint16(portBuf)

	return net.JoinHostPort(addr, strconv.Itoa(int(port))), nil
}

func sendSocks5Response(conn net.Conn, reply byte, addr string, port uint16) error {
	// Parse address
	ip := net.ParseIP(addr)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", addr)
	}

	var addrType byte
	var addrBytes []byte

	if ip4 := ip.To4(); ip4 != nil {
		addrType = ipv4
		addrBytes = ip4
	} else {
		addrType = ipv6
		addrBytes = ip.To16()
	}

	// Build response
	response := []byte{socks5Version, reply, 0x00, addrType}
	response = append(response, addrBytes...)

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, port)
	response = append(response, portBytes...)

	_, err := conn.Write(response)
	return err
}

func proxyData(client, target net.Conn) {
	// Use v2-style proxy: wait for ONE direction to finish (avoids deadlock)
	done := make(chan struct{}, 2)

	// Client to target
	go func() {
		defer func() { done <- struct{}{} }()
		_, err := io.Copy(target, client)
		if err != nil && err != io.EOF {
			// Log error but continue
		}
		// Close write side to signal completion
		if conn, ok := target.(*net.TCPConn); ok {
			conn.CloseWrite()
		}
	}()

	// Target to client
	go func() {
		defer func() { done <- struct{}{} }()
		_, err := io.Copy(client, target)
		if err != nil && err != io.EOF {
			// Log error but continue
		}
		// Close write side to signal completion
		if conn, ok := client.(*net.TCPConn); ok {
			conn.CloseWrite()
		}
	}()

	// Wait for ONE direction to complete (v2 approach)
	<-done
}
