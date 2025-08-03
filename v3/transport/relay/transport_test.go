package relay

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/acheong08/syndicate/v3/transport"
)

func TestTransportName(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	if tr.Name() != "relay" {
		t.Errorf("Name() = %v, want relay", tr.Name())
	}
}

func TestTransportConfig(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	// Test default config
	tr := NewTransport(cert, Config{})
	if tr.config.ConnectTimeout != 30*time.Second {
		t.Errorf("Default ConnectTimeout = %v, want 30s", tr.config.ConnectTimeout)
	}
	if tr.config.RelayTimeout != 30*time.Second {
		t.Errorf("Default RelayTimeout = %v, want 30s", tr.config.RelayTimeout)
	}

	// Test custom config
	customConfig := Config{
		Config: transport.Config{
			ConnectTimeout: 15 * time.Second,
		},
		RelayTimeout: 45 * time.Second,
		DiscoveryConfig: transport.DiscoveryConfig{
			Certificate: cert,
			CacheTTL:    10 * time.Minute,
		},
	}

	tr2 := NewTransport(cert, customConfig)
	if tr2.config.ConnectTimeout != 15*time.Second {
		t.Errorf("Custom ConnectTimeout = %v, want 15s", tr2.config.ConnectTimeout)
	}
	if tr2.config.RelayTimeout != 45*time.Second {
		t.Errorf("Custom RelayTimeout = %v, want 45s", tr2.config.RelayTimeout)
	}
}

func TestRelayEndpointValidation(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	ctx := context.Background()

	// Test invalid endpoint type - create a mock endpoint that's not RelayEndpoint
	invalidEndpoint := &mockEndpoint{}
	_, err = tr.Dial(ctx, invalidEndpoint)
	if err == nil || !strings.Contains(err.Error(), "expected RelayEndpoint") {
		t.Errorf("Expected RelayEndpoint validation error, got: %v", err)
	}
}

func TestTransportClose(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})

	// Test close
	err = tr.Close()
	if err != nil {
		t.Errorf("Close() failed: %v", err)
	}

	// Test operations after close
	ctx := context.Background()
	deviceID := transport.TestDeviceID(cert)
	endpoint := transport.NewRelayEndpointWithRelays(deviceID, "", []string{"relay://test.example.com:22067"})

	_, err = tr.Dial(ctx, endpoint)
	if err == nil || !strings.Contains(err.Error(), "relay transport is closed") {
		t.Errorf("Expected transport closed error, got: %v", err)
	}
}

func TestGetInvite(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	ctx := context.Background()

	// Test invalid URL
	deviceID := transport.TestDeviceID(cert)
	_, err = tr.getInvite(ctx, "invalid-url", deviceID)
	if err == nil || (!strings.Contains(err.Error(), "invalid relay URL") && !strings.Contains(err.Error(), "unsupported relay scheme")) {
		t.Errorf("Expected invalid URL error, got: %v", err)
	}
}

// mockEndpoint is a test endpoint that's not a RelayEndpoint
type mockEndpoint struct{}

func (m *mockEndpoint) Address() string                  { return "mock://test" }
func (m *mockEndpoint) Network() string                  { return "mock" }
func (m *mockEndpoint) Metadata() map[string]interface{} { return nil }
