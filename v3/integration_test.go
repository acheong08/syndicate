package v3

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/acheong08/syndicate/v3/syndicate"
	"github.com/syncthing/syncthing/lib/protocol"
)

func TestHTTPServerSOCKS5Integration(t *testing.T) {
	// Skip if we can't connect to real relays
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create certificates for server and client
	serverCert, err := generateTestCert()
	if err != nil {
		t.Fatalf("Failed to generate server cert: %v", err)
	}

	clientCert, err := generateTestCert()
	if err != nil {
		t.Fatalf("Failed to generate client cert: %v", err)
	}

	// Get device IDs
	serverDeviceID := protocol.NewDeviceID(serverCert.Certificate[0])
	clientDeviceID := protocol.NewDeviceID(clientCert.Certificate[0])

	t.Logf("Server device ID: %s", serverDeviceID.String())
	t.Logf("Client device ID: %s", clientDeviceID.String())

	// Create HTTP server
	server, err := syndicate.NewServer(syndicate.ServerConfig{
		Certificate:    serverCert,
		DeviceID:       serverDeviceID,
		MaxConnections: 10,
		TrustedDevices: []syndicate.DeviceID{clientDeviceID}, // Trust the client
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Close()

	// Set up HTTP handler that responds to HTTP requests
	httpHandler := func(conn net.Conn, deviceID syndicate.DeviceID) {
		defer conn.Close()
		t.Logf("Received connection from device: %s", deviceID.String())

		// Read HTTP request
		reader := bufio.NewReader(conn)

		// Read the request line
		requestLine, _, err := reader.ReadLine()
		if err != nil {
			t.Logf("Failed to read request line: %v", err)
			return
		}

		t.Logf("Request line: %s", string(requestLine))

		// Read headers until empty line
		for {
			line, _, err := reader.ReadLine()
			if err != nil {
				t.Logf("Failed to read header: %v", err)
				return
			}
			if len(line) == 0 {
				break // End of headers
			}
			t.Logf("Header: %s", string(line))
		}
		// Parse request line
		parts := strings.Split(string(requestLine), " ")
		if len(parts) < 2 {
			writeHTTPError(conn, 400, "Bad Request")
			return
		}

		method := parts[0]
		path := parts[1]

		t.Logf("HTTP %s %s", method, path)

		// Handle different paths
		switch path {
		case "/":
			writeHTTPResponse(conn, 200, "text/plain", "Hello from HTTP server!")
		case "/test":
			writeHTTPResponse(conn, 200, "application/json", `{"status": "ok", "message": "test endpoint"}`)
		default:
			writeHTTPError(conn, 404, "Not Found")
		}
	}

	// Start the server
	go func() {
		if err := server.HandleConnections(httpHandler); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	// Wait for server to start and discovery to propagate
	// Discovery announcements need time to propagate through the network
	t.Log("Waiting for discovery to propagate...")
	time.Sleep(8 * time.Second)

	// Create client
	client, err := syndicate.NewClient(syndicate.ClientConfig{
		Certificate:       clientCert,
		DeviceID:          clientDeviceID,
		MaxConnections:    10,
		ConnectionTimeout: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Test direct connection with retry for relay session conflicts
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Log("Testing direct syndicate connection...")

	var conn net.Conn
	var lastErr error

	// Retry connection in case of relay session conflicts
	for attempt := 1; attempt <= 3; attempt++ {
		t.Logf("Connection attempt %d/3...", attempt)
		conn, lastErr = client.Connect(ctx, serverDeviceID)
		if lastErr == nil {
			break
		}

		if attempt < 3 {
			t.Logf("Connection attempt %d failed: %v, retrying...", attempt, lastErr)
			time.Sleep(time.Duration(attempt) * time.Second) // Progressive backoff
		}
	}

	if lastErr != nil {
		t.Fatalf("Failed to connect to server after 3 attempts: %v", lastErr)
	}
	defer conn.Close()
	// Send a simple HTTP request
	httpReq := "GET / HTTP/1.1\r\n" +
		"Host: " + serverDeviceID.String() + ".syncthing\r\n" +
		"Connection: close\r\n" +
		"\r\n"

	// Write request
	_, writeErr := conn.Write([]byte(httpReq))
	if writeErr != nil {
		t.Fatalf("Failed to write HTTP request: %v", writeErr)
	}

	// Read response with timeout
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	response := make([]byte, 4096)
	n, readErr := conn.Read(response)
	if readErr != nil && readErr != io.EOF {
		t.Fatalf("Failed to read HTTP response: %v", readErr)
	}

	responseStr := string(response[:n])
	t.Logf("Received response (%d bytes):\n%s", n, responseStr)

	// Basic validation
	if n == 0 {
		t.Fatal("Received empty response")
	}

	if !contains(responseStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected HTTP 200 OK response, got: %s", responseStr)
	}

	if !contains(responseStr, "Hello from HTTP server!") {
		t.Errorf("Expected response body, got: %s", responseStr)
	}

	t.Log("✅ Direct syndicate connection test passed!")

	// Test the /test endpoint
	t.Log("Testing /test endpoint...")

	conn2, connectErr := client.Connect(ctx, serverDeviceID)
	if connectErr != nil {
		t.Fatalf("Failed to connect for second test: %v", connectErr)
	}
	defer conn2.Close()

	httpReq2 := "GET /test HTTP/1.1\r\n" +
		"Host: " + serverDeviceID.String() + ".syncthing\r\n" +
		"Connection: close\r\n" +
		"\r\n"

	_, writeErr2 := conn2.Write([]byte(httpReq2))
	if writeErr2 != nil {
		t.Fatalf("Failed to write second HTTP request: %v", writeErr2)
	}

	conn2.SetReadDeadline(time.Now().Add(5 * time.Second))
	response2 := make([]byte, 4096)
	n2, readErr2 := conn2.Read(response2)
	if readErr2 != nil && readErr2 != io.EOF {
		t.Fatalf("Failed to read second HTTP response: %v", readErr2)
	}

	responseStr2 := string(response2[:n2])
	t.Logf("Received /test response (%d bytes):\n%s", n2, responseStr2)

	if !contains(responseStr2, "HTTP/1.1 200 OK") {
		t.Errorf("Expected HTTP 200 OK response for /test, got: %s", responseStr2)
	}

	if !contains(responseStr2, `"status": "ok"`) {
		t.Errorf("Expected JSON response body for /test, got: %s", responseStr2)
	}

	t.Log("✅ /test endpoint test passed!")
	t.Log("✅ All integration tests passed!")
}

func writeHTTPResponse(conn net.Conn, statusCode int, contentType, body string) {
	statusText := http.StatusText(statusCode)
	response := fmt.Sprintf("HTTP/1.1 %d %s\r\n", statusCode, statusText) +
		fmt.Sprintf("Content-Type: %s\r\n", contentType) +
		fmt.Sprintf("Content-Length: %d\r\n", len(body)) +
		"Connection: close\r\n" +
		"\r\n" +
		body

	conn.Write([]byte(response))
}

func writeHTTPError(conn net.Conn, statusCode int, statusText string) {
	body := fmt.Sprintf("%d %s", statusCode, statusText)
	writeHTTPResponse(conn, statusCode, "text/plain", body)
}

func generateTestCert() (tls.Certificate, error) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Syndicate Test"},
			Country:      []string{"US"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
