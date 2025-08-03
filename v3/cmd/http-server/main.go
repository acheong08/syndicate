package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/acheong08/syndicate/v3/syndicate"
	"github.com/syncthing/syncthing/lib/protocol"
)

type Config struct {
	ListenAddr string `json:"listen_addr"`
	DeviceID   string `json:"device_id"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
}

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
		ListenAddr: ":8080",
		DeviceID:   "",
		CertFile:   "",
		KeyFile:    "",
	}
}

func main() {
	var (
		configFile = flag.String("config", "", "Path to config file (JSON format)")
		listenAddr = flag.String("listen", ":8080", "HTTP listen address")
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
	log.Printf("Starting HTTP server with device ID: %s", deviceID.String())
	log.Printf("Listening on: %s", config.ListenAddr)

	// Create syndicate server
	server, err := createSyndicateServer(cert, deviceID)
	if err != nil {
		log.Fatalf("Failed to create syndicate server: %v", err)
	}
	defer server.Close()

	// Create HTTP server
	httpServer := createHTTPServer(config.ListenAddr, deviceID)

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start syndicate server
	go func() {
		log.Printf("Starting syndicate server...")
		if err := server.HandleConnections(func(conn net.Conn, deviceID syndicate.DeviceID) {
			log.Printf("New connection from device: %s", deviceID.String())
			log.Printf("Incoming syndicate conn from %s -> proxy to %s", conn.RemoteAddr(), config.ListenAddr)
			// For HTTP server, we'll proxy connections to our local HTTP server
			handleSyndicateConnection(conn, config.ListenAddr)
		}); err != nil {
			log.Printf("Syndicate server error: %v", err)
			cancel()
		}
	}()

	// Start HTTP server
	go func() {
		log.Printf("Starting HTTP server on %s", config.ListenAddr)
		var err error
		if config.CertFile != "" && config.KeyFile != "" {
			err = httpServer.ListenAndServeTLS(config.CertFile, config.KeyFile)
		} else {
			err = httpServer.ListenAndServe()
		}
		if err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
			cancel()
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		log.Printf("Received signal: %v", sig)
	case <-ctx.Done():
		log.Printf("Context cancelled")
	}

	// Graceful shutdown
	log.Printf("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	cancel()
	log.Printf("Shutdown complete")
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
			Organization:  []string{"Syndicate HTTP Server"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
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

func createSyndicateServer(cert tls.Certificate, deviceID syndicate.DeviceID) (syndicate.Server, error) {
	// Create syndicate server
	server, err := syndicate.NewServer(syndicate.ServerConfig{
		Certificate:    cert,
		DeviceID:       deviceID,
		MaxConnections: 100,
		Country:        "", // Auto-detect
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create syndicate server: %w", err)
	}

	return server, nil
}

func createHTTPServer(listenAddr string, deviceID syndicate.DeviceID) *http.Server {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":    "healthy",
			"device_id": deviceID.String(),
			"time":      time.Now().UTC().Format(time.RFC3339),
		})
	})

	// Info endpoint
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"service":   "syndicate-http-server",
			"version":   "v3",
			"device_id": deviceID.String(),
			"time":      time.Now().UTC().Format(time.RFC3339),
			"headers":   r.Header,
			"method":    r.Method,
			"url":       r.URL.String(),
			"remote":    r.RemoteAddr,
		})
	})

	// Echo endpoint
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Echo: %s %s\n", r.Method, r.URL.Path)
		fmt.Fprintf(w, "Device ID: %s\n", deviceID.String())
		fmt.Fprintf(w, "Headers:\n")
		for k, v := range r.Header {
			fmt.Fprintf(w, "  %s: %v\n", k, v)
		}
		fmt.Fprintf(w, "Remote: %s\n", r.RemoteAddr)
	})

	// Root endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Syndicate HTTP Server</title>
</head>
<body>
    <h1>Syndicate HTTP Server</h1>
    <p>This HTTP server is running over the Syndicate transport mechanism.</p>
    <p><strong>Device ID:</strong> %s</p>
    <ul>
        <li><a href="/health">Health Check</a></li>
        <li><a href="/info">Server Info</a></li>
        <li><a href="/echo">Echo Request</a></li>
    </ul>
    <p>Time: %s</p>
</body>
</html>`, deviceID.String(), time.Now().UTC().Format(time.RFC3339))
		} else {
			http.NotFound(w, r)
		}
	})

	return &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  0,                 // Disable read timeout to prevent broken pipe
		WriteTimeout: 0,                 // Disable write timeout to prevent broken pipe
		IdleTimeout:  300 * time.Second, // 5 minutes for connection reuse
	}
}

// handleSyndicateConnection proxies a syndicate connection to the local HTTP server
func handleSyndicateConnection(syndicateConn net.Conn, httpAddr string) {
	defer syndicateConn.Close()

	// Connect to local HTTP server
	httpConn, err := net.Dial("tcp", httpAddr)
	if err != nil {
		log.Printf("Failed to connect to HTTP server: %v", err)
		return
	}
	defer httpConn.Close()
	log.Println("Dialing local HTTP server success")

	// Use v2-style proxy: wait for ONE direction to finish (like v2)
	done := make(chan struct{}, 2)

	syndicateConn.Write([]byte("Hello world"))

	// Copy from syndicate to HTTP server
	go func() {
		defer func() { done <- struct{}{} }()
		_, err := io.Copy(httpConn, syndicateConn)
		if err != nil && err != io.EOF {
			log.Printf("Error copying syndicate->HTTP: %v", err)
		}
		// Close write side to signal completion
		if conn, ok := httpConn.(*net.TCPConn); ok {
			conn.CloseWrite()
		}
	}()

	// Copy from HTTP server to syndicate
	go func() {
		defer func() { done <- struct{}{} }()
		_, err := io.Copy(syndicateConn, httpConn)
		if err != nil && err != io.EOF {
			log.Printf("Error copying HTTP->syndicate: %v", err)
		}
		// Close write side to signal completion
		if conn, ok := syndicateConn.(*net.TCPConn); ok {
			conn.CloseWrite()
		}
	}()

	// Wait for ONE direction to finish (v2 approach)
	<-done
	log.Println("Successfully copied connections")
}
