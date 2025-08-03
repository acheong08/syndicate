package mux

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/acheong08/syndicate/v3/transport"
	"github.com/acheong08/syndicate/v3/transport/relay"
)

func createTestTransport() transport.Transport {
	cert, err := transport.TestCertificate("test")
	if err != nil {
		panic(fmt.Sprintf("Failed to create test certificate: %v", err))
	}

	config := relay.Config{
		Config: transport.DefaultConfig(),
	}

	return relay.NewTransport(cert, config)
}

func TestManagerBasic(t *testing.T) {
	config := DefaultConfig()
	config.IdleTimeout = 100 * time.Millisecond
	transport := createTestTransport()
	defer transport.Close()

	manager := NewManager(config, transport)
	defer manager.Close()

	// Test initial state
	stats := manager.Statistics()
	if len(stats) != 0 {
		t.Errorf("Initial connections = %d, want 0", len(stats))
	}
}

func TestManagerDialAndListen(t *testing.T) {
	// Create a listener for testing
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	config := DefaultConfig()
	config.KeepAliveInterval = 0

	serverTransport := createTestTransport()
	defer serverTransport.Close()

	serverManager := NewManager(config, serverTransport)
	defer serverManager.Close()

	clientTransport := createTestTransport()
	defer clientTransport.Close()

	clientManager := NewManager(config, clientTransport)
	defer clientManager.Close()

	// Test opening session (note: this would need real network connectivity)
	// For integration testing, we'll skip the actual dial test
	t.Skip("Integration test requires real network connectivity")
}

func TestManagerWithMockDialer(t *testing.T) {
	config := DefaultConfig()
	config.IdleTimeout = 100 * time.Millisecond

	// Use the dialer-based constructor for testing
	manager := NewManagerWithDialer(config, func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, fmt.Errorf("mock dialer: not implemented")
	})
	defer manager.Close()

	// Test basic functionality - just verify the manager was created
	stats := manager.Statistics()
	if len(stats) != 0 {
		t.Errorf("Initial connections = %d, want 0", len(stats))
	}
}
