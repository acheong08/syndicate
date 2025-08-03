// Package syndicate provides high-level APIs for Syncthing-based networking
// applications. It builds on the v3 mux and transport layers to provide
// simple interfaces for common networking patterns.
package syndicate

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

// DeviceID represents a Syncthing device identifier
type DeviceID = protocol.DeviceID

// Client provides a high-level interface for creating outbound connections
// to other Syncthing devices using multiplexed streams over various transports.
type Client interface {
	// Connect establishes a multiplexed stream to the target device.
	// Returns a net.Conn that represents a single stream within a
	// multiplexed connection. Multiple calls to Connect with the same
	// device will reuse the underlying connection when possible.
	Connect(ctx context.Context, targetDevice DeviceID) (net.Conn, error)

	// ConnectWithPriority establishes a connection with a specific priority.
	// Higher priority streams may receive preferential treatment for bandwidth.
	ConnectWithPriority(ctx context.Context, targetDevice DeviceID, priority uint8) (net.Conn, error)

	// Stats returns statistics about active connections and data transfer.
	Stats() ClientStats

	// Close closes all connections and releases resources.
	Close() error
}

// Server provides a high-level interface for accepting inbound connections
// from other Syncthing devices.
type Server interface {
	// HandleConnections starts accepting connections and calls the handler
	// function for each new virtual connection. The handler runs in its own
	// goroutine, so it can block without affecting other connections.
	HandleConnections(handler ConnectionHandler) error

	// Stats returns statistics about active connections and data transfer.
	Stats() ServerStats

	// Close stops accepting new connections and closes existing ones.
	Close() error
}

// Proxy provides high-level interfaces for common proxy patterns like
// SOCKS5 proxies, HTTP proxies, and TCP tunnels.
type Proxy interface {
	// Start begins listening for connections and proxying them.
	Start(ctx context.Context) error

	// Stats returns proxy statistics.
	Stats() ProxyStats

	// Close stops the proxy and closes all connections.
	Close() error
}

// ConnectionHandler is called for each new virtual connection on a server.
// The deviceID parameter identifies which device initiated the connection.
// The handler should close the connection when done.
type ConnectionHandler func(conn net.Conn, deviceID DeviceID)

// ClientConfig configures a Client instance.
type ClientConfig struct {
	// Certificate is the TLS certificate used for authentication
	Certificate tls.Certificate

	// DeviceID is this client's device identifier (derived from certificate if not set)
	DeviceID DeviceID

	// Country code for relay selection (e.g., "US", "DE"). Auto-detected if empty.
	Country string

	// MaxConnections limits concurrent connections to different devices
	MaxConnections int

	// MaxStreamsPerConnection limits streams per multiplexed connection
	MaxStreamsPerConnection int

	// ConnectionTimeout for establishing new connections
	ConnectionTimeout time.Duration

	// IdleTimeout for cleaning up unused connections
	IdleTimeout time.Duration

	// KeepAliveInterval for connection health checks
	KeepAliveInterval time.Duration
}

// ServerConfig configures a Server instance.
type ServerConfig struct {
	// Certificate is the TLS certificate used for authentication
	Certificate tls.Certificate

	// DeviceID is this server's device identifier (derived from certificate if not set)
	DeviceID DeviceID

	// TrustedDevices lists device IDs allowed to connect (empty = allow all)
	TrustedDevices []DeviceID

	// Country code for relay selection (e.g., "US", "DE"). Auto-detected if empty.
	Country string

	// MaxConnections limits concurrent device connections
	MaxConnections int

	// MaxStreamsPerConnection limits streams per multiplexed connection
	MaxStreamsPerConnection int

	// IdleTimeout for cleaning up unused connections
	IdleTimeout time.Duration

	// KeepAliveInterval for connection health checks
	KeepAliveInterval time.Duration
}

// ProxyConfig configures a Proxy instance.
type ProxyConfig struct {
	// Type of proxy: "socks5", "http", "tunnel"
	Type string

	// LocalAddr to listen on (e.g., ":1080" for SOCKS5)
	LocalAddr string

	// Certificate for authentication
	Certificate tls.Certificate

	// DeviceID of this proxy (derived from certificate if not set)
	DeviceID DeviceID

	// For client proxies: target device to connect to
	TargetDevice DeviceID

	// For server proxies: trusted devices (empty = allow all)
	TrustedDevices []DeviceID

	// Country code for relay selection
	Country string

	// Connection pooling and timeout settings
	MaxConnections          int
	MaxStreamsPerConnection int
	ConnectionTimeout       time.Duration
	IdleTimeout             time.Duration
}

// ClientStats provides statistics about a Client's operations.
type ClientStats struct {
	// Connection statistics
	TotalConnections  int
	ActiveConnections int
	FailedConnections uint64

	// Stream statistics
	TotalStreams  int
	ActiveStreams int
	FailedStreams uint64

	// Data transfer statistics
	BytesSent     uint64
	BytesReceived uint64

	// Performance metrics
	AverageRTT           time.Duration
	ConnectionsPerSecond float64
}

// ServerStats provides statistics about a Server's operations.
type ServerStats struct {
	// Connection statistics
	TotalConnections    int
	ActiveConnections   int
	RejectedConnections uint64

	// Stream statistics
	TotalStreams  int
	ActiveStreams int

	// Data transfer statistics
	BytesSent     uint64
	BytesReceived uint64

	// Performance metrics
	AverageRTT           time.Duration
	ConnectionsPerSecond float64

	// Device statistics
	ConnectedDevices int
	UniqueDevices    uint64
}

// ProxyStats provides statistics about a Proxy's operations.
type ProxyStats struct {
	// Proxy-level statistics
	TotalSessions    uint64
	ActiveSessions   int
	FailedSessions   uint64
	RejectedSessions uint64

	// Underlying connection statistics
	TotalConnections  int
	ActiveConnections int

	// Data transfer statistics
	BytesSent     uint64
	BytesReceived uint64

	// Performance metrics
	SessionsPerSecond      float64
	AverageSessionDuration time.Duration
}

// Default configuration values
const (
	DefaultMaxConnections          = 10
	DefaultMaxStreamsPerConnection = 100
	DefaultConnectionTimeout       = 30 * time.Second
	DefaultIdleTimeout             = 5 * time.Minute
	DefaultKeepAliveInterval       = 30 * time.Second
)

// Common proxy types
const (
	ProxyTypeSOCKS5 = "socks5"
	ProxyTypeHTTP   = "http"
	ProxyTypeTunnel = "tunnel"
)
