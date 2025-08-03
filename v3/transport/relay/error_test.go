package relay

import (
	"context"
	"testing"
	"time"

	"github.com/acheong08/syndicate/v3/transport"
)

// TestErrorHandling tests various error conditions
func TestErrorHandling(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})
	defer tr.Close()

	ctx := context.Background()

	// Test dial with invalid endpoint type
	_, err = tr.Dial(ctx, &mockEndpoint{})
	if err == nil {
		t.Error("Expected error when dialing with invalid endpoint type")
	}

	// Test dial with closed transport
	tr.Close()
	endpoint, _ := transport.NewRelayEndpoint("MFZWI3DBONSGC3THMFZGK5TFMQ3GC3THMFZGK5TFMQ3GC3THMFZGK5TFON4Q.syncthing")
	_, err = tr.Dial(ctx, endpoint)
	if err == nil {
		t.Error("Expected error when dialing with closed transport")
	}
}

// TestPanicsAndRecovery tests panic conditions
func TestPanicsAndRecovery(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Unexpected panic: %v", r)
		}
	}()

	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	// Test various nil/invalid inputs
	tr := NewTransport(cert, Config{})
	defer tr.Close()

	ctx := context.Background()

	// Test with valid endpoint (should not panic, but will fail)
	endpoint, _ := transport.NewRelayEndpoint(transport.TestDeviceID(cert).String() + ".syncthing")
	_, err = tr.Dial(ctx, endpoint)
	if err == nil {
		t.Error("Expected error with unreachable endpoint")
	}
}

// TestResourceCleanup tests that resources are properly cleaned up
func TestResourceCleanup(t *testing.T) {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		t.Fatalf("Failed to create test certificate: %v", err)
	}

	tr := NewTransport(cert, Config{})

	ctx, cancel := context.WithCancel(context.Background())

	// Create listener
	listener, err := NewListener(ctx, tr, ListenConfig{})
	if err != nil {
		t.Fatalf("NewListener() error = %v", err)
	}

	// Cancel context
	cancel()

	// Wait a bit for cleanup
	time.Sleep(10 * time.Millisecond)

	// Close listener
	if err := listener.Close(); err != nil {
		t.Errorf("Listener close error: %v", err)
	}

	// Close transport
	if err := tr.Close(); err != nil {
		t.Errorf("Transport close error: %v", err)
	}

	// Operations after close should fail gracefully
	_, err = tr.Dial(context.Background(), &mockEndpoint{})
	if err == nil {
		t.Error("Expected error after transport close")
	}
}
