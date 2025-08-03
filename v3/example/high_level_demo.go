package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/acheong08/syndicate/v3/syndicate"
	"github.com/syncthing/syncthing/lib/protocol"
)

func main() {
	// This is a basic example showing how to use the v3 syndicate high-level API

	// Create a certificate (in practice, you'd load this from files)
	cert, err := generateTestCertificate()
	if err != nil {
		log.Fatalf("Failed to generate certificate: %v", err)
	}

	// Example 1: Client connecting to a server
	fmt.Println("=== Client Example ===")
	clientExample(cert)

	// Example 2: Server accepting connections
	fmt.Println("\n=== Server Example ===")
	serverExample(cert)

	// Example 3: SOCKS5 proxy
	fmt.Println("\n=== Proxy Example ===")
	proxyExample(cert)
}

func clientExample(cert tls.Certificate) {
	// Create a client
	client, err := syndicate.NewClient(syndicate.ClientConfig{
		Certificate:       cert,
		MaxConnections:    5,
		ConnectionTimeout: 30 * time.Second,
	})
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		return
	}
	defer client.Close()

	// Parse a target device ID (this would be a real device ID in practice)
	targetDeviceID, err := protocol.DeviceIDFromString("EXAMPLE-DEVICE-ID-WOULD-GO-HERE-WITH-REAL-ID")
	if err != nil {
		log.Printf("Note: Using example device ID (would fail in practice): %v", err)
		// Create a dummy device ID for this example
		targetDeviceID = protocol.NewDeviceID(cert.Certificate[0])
	}

	// Connect to the target device
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := client.Connect(ctx, targetDeviceID)
	if err != nil {
		log.Printf("Failed to connect (expected in example): %v", err)
		return
	}
	defer conn.Close()

	// Use the connection like any net.Conn
	_, err = conn.Write([]byte("Hello from v3 client!"))
	if err != nil {
		log.Printf("Failed to write: %v", err)
		return
	}

	// Show client statistics
	stats := client.Stats()
	fmt.Printf("Client stats: %d total connections, %d active streams\n",
		stats.TotalConnections, stats.ActiveStreams)
}

func serverExample(cert tls.Certificate) {
	// Create a server
	server, err := syndicate.NewServer(syndicate.ServerConfig{
		Certificate:    cert,
		MaxConnections: 10,
	})
	if err != nil {
		log.Printf("Failed to create server: %v", err)
		return
	}
	defer server.Close()

	// Define connection handler
	handler := func(conn net.Conn, deviceID syndicate.DeviceID) {
		defer conn.Close()

		fmt.Printf("Accepted connection from device: %s\n", deviceID.Short())

		// Echo server example
		buffer := make([]byte, 1024)
		for {
			n, err := conn.Read(buffer)
			if err != nil {
				break
			}

			_, err = conn.Write(buffer[:n])
			if err != nil {
				break
			}
		}
	}

	// Start handling connections
	err = server.HandleConnections(handler)
	if err != nil {
		log.Printf("Failed to start server (expected in example): %v", err)
		return
	}

	// Show server statistics
	stats := server.Stats()
	fmt.Printf("Server stats: %d total connections, %d active streams\n",
		stats.TotalConnections, stats.ActiveStreams)
}

func proxyExample(cert tls.Certificate) {
	// Create a SOCKS5 proxy that tunnels to a target device
	targetDeviceID := protocol.NewDeviceID(cert.Certificate[0]) // Using self as example

	proxy, err := syndicate.NewProxy(syndicate.ProxyConfig{
		Type:         syndicate.ProxyTypeSOCKS5,
		LocalAddr:    ":1080",
		Certificate:  cert,
		TargetDevice: targetDeviceID,
	})
	if err != nil {
		log.Printf("Failed to create proxy: %v", err)
		return
	}
	defer proxy.Close()

	// Start the proxy
	ctx := context.Background()
	err = proxy.Start(ctx)
	if err != nil {
		log.Printf("Failed to start proxy (expected in example): %v", err)
		return
	}

	// Show proxy statistics
	stats := proxy.Stats()
	fmt.Printf("Proxy stats: %d total sessions, %d active connections\n",
		stats.TotalSessions, stats.ActiveConnections)

	fmt.Println("SOCKS5 proxy would be listening on :1080")
}

// generateTestCertificate creates a test certificate for the example
func generateTestCertificate() (tls.Certificate, error) {
	// This is a simplified version - in practice you'd use crypto/cert.go
	// or load certificates from files

	// For now, return an empty certificate - this will cause errors but shows the API
	return tls.Certificate{}, fmt.Errorf("test certificate generation not implemented - use real certificates")
}
