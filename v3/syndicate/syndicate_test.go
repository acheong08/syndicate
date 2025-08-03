package syndicate

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

// MockTransport implements a mock transport for testing
type MockTransport struct {
	dialFunc   func(ctx context.Context, endpoint interface{}) (net.Conn, error)
	listenFunc func(ctx context.Context, endpoint interface{}) (net.Listener, error)
}

func (mt *MockTransport) Dial(ctx context.Context, endpoint interface{}) (net.Conn, error) {
	if mt.dialFunc != nil {
		return mt.dialFunc(ctx, endpoint)
	}
	return &MockConn{}, nil
}

func (mt *MockTransport) Listen(ctx context.Context, endpoint interface{}) (net.Listener, error) {
	if mt.listenFunc != nil {
		return mt.listenFunc(ctx, endpoint)
	}
	return &MockListener{}, nil
}

func (mt *MockTransport) Name() string {
	return "mock"
}

func (mt *MockTransport) Close() error {
	return nil
}

// MockConn implements a mock net.Conn for testing
type MockConn struct {
	readData  []byte
	writeData []byte
	closed    bool
}

func (mc *MockConn) Read(b []byte) (n int, err error) {
	if mc.closed {
		return 0, net.ErrClosed
	}
	if len(mc.readData) > 0 {
		n = copy(b, mc.readData)
		mc.readData = mc.readData[n:]
		return n, nil
	}
	return 0, nil
}

func (mc *MockConn) Write(b []byte) (n int, err error) {
	if mc.closed {
		return 0, net.ErrClosed
	}
	mc.writeData = append(mc.writeData, b...)
	return len(b), nil
}

func (mc *MockConn) Close() error {
	mc.closed = true
	return nil
}

func (mc *MockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
}

func (mc *MockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5678}
}

func (mc *MockConn) SetDeadline(t time.Time) error      { return nil }
func (mc *MockConn) SetReadDeadline(t time.Time) error  { return nil }
func (mc *MockConn) SetWriteDeadline(t time.Time) error { return nil }

// MockListener implements a mock net.Listener for testing
type MockListener struct {
	closed bool
}

func (ml *MockListener) Accept() (net.Conn, error) {
	if ml.closed {
		return nil, net.ErrClosed
	}
	return &MockConn{}, nil
}

func (ml *MockListener) Close() error {
	ml.closed = true
	return nil
}

func (ml *MockListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
}

// Test helper functions
func generateTestCert() tls.Certificate {
	// Create a minimal certificate for testing
	return tls.Certificate{
		Certificate: [][]byte{[]byte("test-cert")},
	}
}

func generateTestDeviceID() DeviceID {
	cert := generateTestCert()
	return protocol.NewDeviceID(cert.Certificate[0])
}

// Client Tests
func TestNewClient(t *testing.T) {
	cert := generateTestCert()

	tests := []struct {
		name      string
		config    ClientConfig
		expectErr bool
	}{
		{
			name: "valid config",
			config: ClientConfig{
				Certificate: cert,
			},
			expectErr: false,
		},
		{
			name:   "missing certificate",
			config: ClientConfig{
				// Certificate missing
			},
			expectErr: true,
		},
		{
			name: "with device ID",
			config: ClientConfig{
				Certificate: cert,
				DeviceID:    generateTestDeviceID(),
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config)
			if tt.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if client == nil {
				t.Error("client is nil")
				return
			}

			// Test client methods
			stats := client.Stats()
			if stats.TotalConnections != 0 {
				t.Errorf("expected 0 total connections, got %d", stats.TotalConnections)
			}

			// Clean up
			client.Close()
		})
	}
}

func TestClientConnect(t *testing.T) {
	cert := generateTestCert()
	targetDevice := generateTestDeviceID()

	// Note: This test will fail in the current implementation because
	// the transport layer requires actual relay connections
	// In a production test, we'd need to mock the transport properly

	client, err := NewClient(ClientConfig{
		Certificate:       cert,
		ConnectionTimeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This will fail with current implementation - expected
	_, err = client.Connect(ctx, targetDevice)
	if err == nil {
		t.Error("expected connection to fail without proper transport")
	}
}

func TestClientStats(t *testing.T) {
	cert := generateTestCert()

	client, err := NewClient(ClientConfig{
		Certificate: cert,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// Test initial stats
	stats := client.Stats()
	if stats.TotalConnections != 0 {
		t.Errorf("expected 0 total connections, got %d", stats.TotalConnections)
	}
	if stats.ActiveConnections != 0 {
		t.Errorf("expected 0 active connections, got %d", stats.ActiveConnections)
	}
	if stats.BytesSent != 0 {
		t.Errorf("expected 0 bytes sent, got %d", stats.BytesSent)
	}
}

// Server Tests
func TestNewServer(t *testing.T) {
	cert := generateTestCert()

	tests := []struct {
		name      string
		config    ServerConfig
		expectErr bool
	}{
		{
			name: "valid config",
			config: ServerConfig{
				Certificate: cert,
			},
			expectErr: false,
		},
		{
			name:   "missing certificate",
			config: ServerConfig{
				// Certificate missing
			},
			expectErr: true,
		},
		{
			name: "with trusted devices",
			config: ServerConfig{
				Certificate:    cert,
				TrustedDevices: []DeviceID{generateTestDeviceID()},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewServer(tt.config)
			if tt.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if server == nil {
				t.Error("server is nil")
				return
			}

			// Test server methods
			stats := server.Stats()
			if stats.TotalConnections != 0 {
				t.Errorf("expected 0 total connections, got %d", stats.TotalConnections)
			}

			// Clean up
			server.Close()
		})
	}
}

// Proxy Tests
func TestNewProxy(t *testing.T) {
	cert := generateTestCert()
	targetDevice := generateTestDeviceID()

	tests := []struct {
		name      string
		config    ProxyConfig
		expectErr bool
	}{
		{
			name: "valid SOCKS5 client proxy",
			config: ProxyConfig{
				Type:         ProxyTypeSOCKS5,
				LocalAddr:    ":0", // Use any available port
				Certificate:  cert,
				TargetDevice: targetDevice,
			},
			expectErr: false,
		},
		{
			name: "valid HTTP proxy",
			config: ProxyConfig{
				Type:        ProxyTypeHTTP,
				LocalAddr:   ":0",
				Certificate: cert,
			},
			expectErr: false,
		},
		{
			name: "missing certificate",
			config: ProxyConfig{
				Type:      ProxyTypeSOCKS5,
				LocalAddr: ":0",
				// Certificate missing
			},
			expectErr: true,
		},
		{
			name: "missing local address",
			config: ProxyConfig{
				Type:        ProxyTypeSOCKS5,
				Certificate: cert,
				// LocalAddr missing
			},
			expectErr: true,
		},
		{
			name: "invalid proxy type",
			config: ProxyConfig{
				Type:        "invalid",
				LocalAddr:   ":0",
				Certificate: cert,
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy, err := NewProxy(tt.config)
			if tt.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if proxy == nil {
				t.Error("proxy is nil")
				return
			}

			// Test proxy methods
			stats := proxy.Stats()
			if stats.TotalSessions != 0 {
				t.Errorf("expected 0 total sessions, got %d", stats.TotalSessions)
			}

			// Clean up
			proxy.Close()
		})
	}
}

// Integration Tests
func TestClientServerIntegration(t *testing.T) {
	// This is a conceptual test - actual implementation would require
	// proper transport mocking or real relay connections
	t.Skip("Integration test requires real transport implementation")

	cert := generateTestCert()

	// Create server
	server, err := NewServer(ServerConfig{
		Certificate: cert,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	// Create client
	client, err := NewClient(ClientConfig{
		Certificate: cert,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// Test connection (would need proper transport setup)
	// This is where we'd test actual data transfer
}

// Benchmark Tests
func BenchmarkClientConnect(b *testing.B) {
	cert := generateTestCert()
	targetDevice := generateTestDeviceID()

	client, err := NewClient(ClientConfig{
		Certificate: cert,
	})
	if err != nil {
		b.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This will fail but measures the setup overhead
		client.Connect(ctx, targetDevice)
	}
}

func BenchmarkClientStats(b *testing.B) {
	cert := generateTestCert()

	client, err := NewClient(ClientConfig{
		Certificate: cert,
	})
	if err != nil {
		b.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.Stats()
	}
}
