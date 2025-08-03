package main

import (
	"context"
	"fmt"
	"time"

	"github.com/acheong08/syndicate/v3/mux"
	"github.com/acheong08/syndicate/v3/transport"
	"github.com/acheong08/syndicate/v3/transport/relay"
)

// Example demonstrates how to use the v3 mux layer with the transport layer
func mainMuxDemo() {
	// Create a test TLS certificate for the example
	cert, err := transport.TestCertificate("mux-demo")
	if err != nil {
		fmt.Printf("Failed to create certificate: %v\n", err)
		return
	}

	// Configure the relay transport
	relayConfig := relay.Config{
		Config:       transport.DefaultConfig(),
		RelayTimeout: 30 * time.Second,
		DiscoveryConfig: transport.DiscoveryConfig{
			Certificate: cert,
			CacheTTL:    5 * time.Minute,
		},
	}
	relayConfig.ConnectTimeout = 10 * time.Second

	relayTransport := relay.NewTransport(cert, relayConfig)
	defer relayTransport.Close()

	// Configure the mux layer
	muxConfig := mux.DefaultConfig()
	muxConfig.MaxConcurrentStreams = 100
	muxConfig.IdleTimeout = 5 * time.Minute

	// Create the mux manager
	muxManager := mux.NewManager(muxConfig, relayTransport)
	defer muxManager.Close()

	// Example: Create a session to another device
	ctx := context.Background()

	// Create a test endpoint
	deviceID := transport.TestDeviceID(cert)
	endpoint := transport.NewRelayEndpointWithRelays(deviceID, "", nil)

	fmt.Printf("Attempting to dial endpoint: %s\n", endpoint.Address())

	// This would normally connect to a real device
	stream, err := muxManager.DialEndpoint(ctx, endpoint)
	if err != nil {
		fmt.Printf("Failed to dial (expected in demo): %v\n", err)
		return
	}
	defer stream.Close()

	fmt.Println("Successfully created stream (this shouldn't happen in demo)")
}

// Comment out main function to avoid conflicts with high_level_demo.go
// Uncomment to run: go run mux_demo.go
// func main() { mainMuxDemo() }
