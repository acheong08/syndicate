package transport

import (
	"context"
	"net"
	"time"
)

// Transport defines the interface for connection transports
type Transport interface {
	// Dial establishes an outgoing connection to the specified endpoint
	Dial(ctx context.Context, endpoint Endpoint) (net.Conn, error)

	// Listen creates a listener for incoming connections on the specified endpoint
	Listen(ctx context.Context, endpoint Endpoint) (net.Listener, error)

	// Name returns the transport name for identification
	Name() string

	// Close closes the transport and cleans up resources
	Close() error
}

// Endpoint represents a connection endpoint
type Endpoint interface {
	// Address returns the string representation of the endpoint
	Address() string

	// Network returns the network type (e.g., "tcp", "relay")
	Network() string

	// Metadata returns endpoint-specific metadata
	Metadata() map[string]interface{}
}

// Config contains transport configuration
type Config struct {
	// Timeout for connection establishment
	ConnectTimeout time.Duration

	// Keep-alive interval
	KeepAlive time.Duration

	// Transport-specific options
	Options map[string]interface{}
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() Config {
	return Config{
		ConnectTimeout: 30 * time.Second,
		KeepAlive:      30 * time.Second,
		Options:        make(map[string]interface{}),
	}
}
