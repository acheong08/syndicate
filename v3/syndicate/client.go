package syndicate

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/acheong08/syndicate/v3/mux"
	"github.com/acheong08/syndicate/v3/transport"
	"github.com/acheong08/syndicate/v3/transport/relay"
	"github.com/syncthing/syncthing/lib/protocol"
)

// client implements the Client interface using the v3 mux and transport layers.
type client struct {
	config    ClientConfig
	deviceID  DeviceID
	manager   *mux.Manager
	transport transport.Transport

	// Statistics (using atomic for thread safety)
	stats struct {
		totalConnections  int64
		activeConnections int64
		failedConnections uint64
		totalStreams      int64
		activeStreams     int64
		failedStreams     uint64
		bytesSent         uint64
		bytesReceived     uint64

		// Enhanced statistics
		connectionStartTime int64 // Unix nano timestamp of first connection
		lastConnectionTime  int64 // Unix nano timestamp of last connection
		totalRTTMicros      int64 // Sum of RTT measurements in microseconds
		rttSamples          int64 // Number of RTT samples
	}

	mu     sync.RWMutex
	closed bool
}

// NewClient creates a new Client with the given configuration.
func NewClient(config ClientConfig) (Client, error) {
	// Validate and set defaults
	if err := validateClientConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid client config: %w", err)
	}

	// Derive device ID from certificate if not provided
	deviceID := config.DeviceID
	if deviceID == (DeviceID{}) {
		deviceID = protocol.NewDeviceID(config.Certificate.Certificate[0])
	}

	// Create transport (relay transport for now)
	transport := createTransport(config.Certificate, config.Country)

	// Create mux manager with appropriate configuration
	muxConfig := &mux.Config{
		MaxConcurrentStreams: uint32(config.MaxStreamsPerConnection),
		IdleTimeout:          config.IdleTimeout,
		KeepAliveInterval:    config.KeepAliveInterval,
	}
	manager := mux.NewManager(muxConfig, transport)

	return &client{
		config:    config,
		deviceID:  deviceID,
		manager:   manager,
		transport: transport,
	}, nil
}

// Connect establishes a multiplexed stream to the target device.
func (c *client) Connect(ctx context.Context, targetDevice DeviceID) (net.Conn, error) {
	return c.ConnectWithPriority(ctx, targetDevice, 0)
}

// ConnectWithPriority establishes a connection with a specific priority.
func (c *client) ConnectWithPriority(ctx context.Context, targetDevice DeviceID, priority uint8) (net.Conn, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrClientClosed
	}
	c.mu.RUnlock()

	// Record connection attempt timing
	startTime := time.Now()

	// Create endpoint for the target device - use relay endpoint format
	address := fmt.Sprintf("%s.syncthing:0", targetDevice.String())
	ep, err := transport.NewRelayEndpoint(address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse endpoint: %w", err)
	}

	// Apply context timeout if specified
	if c.config.ConnectionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.ConnectionTimeout)
		defer cancel()
	}

	// Dial through the manager using the endpoint
	stream, err := c.manager.DialEndpoint(ctx, ep)
	if err != nil {
		atomic.AddUint64(&c.stats.failedStreams, 1)
		atomic.AddUint64(&c.stats.failedConnections, 1)
		return nil, fmt.Errorf("failed to connect to device %s: %w", targetDevice.Short(), err)
	}

	// Record successful connection timing
	connectionTime := time.Since(startTime)
	atomic.AddInt64(&c.stats.totalRTTMicros, connectionTime.Microseconds())
	atomic.AddInt64(&c.stats.rttSamples, 1)

	// Update connection timestamps
	now := time.Now().UnixNano()
	atomic.StoreInt64(&c.stats.lastConnectionTime, now)
	if atomic.CompareAndSwapInt64(&c.stats.connectionStartTime, 0, now) {
		// First connection
	}

	// Set priority if supported
	if err := stream.SetPriority(priority); err != nil {
		// Priority setting failed, but continue anyway
	}

	// Update statistics
	atomic.AddInt64(&c.stats.totalStreams, 1)
	atomic.AddInt64(&c.stats.activeStreams, 1)

	// Wrap stream to track statistics
	return &trackedConn{
		Stream: stream,
		client: c,
	}, nil
}

// Stats returns statistics about the client's operations.
func (c *client) Stats() ClientStats {
	// Calculate average RTT
	rttSamples := atomic.LoadInt64(&c.stats.rttSamples)
	totalRTT := atomic.LoadInt64(&c.stats.totalRTTMicros)
	var avgRTT time.Duration
	if rttSamples > 0 {
		avgRTT = time.Duration(totalRTT/rttSamples) * time.Microsecond
	}

	// Calculate connections per second
	var connectionsPerSecond float64
	startTime := atomic.LoadInt64(&c.stats.connectionStartTime)
	lastTime := atomic.LoadInt64(&c.stats.lastConnectionTime)
	if startTime > 0 && lastTime > startTime {
		elapsedNanos := lastTime - startTime
		elapsedSeconds := float64(elapsedNanos) / float64(time.Second)
		totalConnections := float64(atomic.LoadInt64(&c.stats.totalStreams))
		if elapsedSeconds > 0 {
			connectionsPerSecond = totalConnections / elapsedSeconds
		}
	}

	return ClientStats{
		TotalConnections:     int(atomic.LoadInt64(&c.stats.totalConnections)),
		ActiveConnections:    int(atomic.LoadInt64(&c.stats.activeConnections)),
		FailedConnections:    atomic.LoadUint64(&c.stats.failedConnections),
		TotalStreams:         int(atomic.LoadInt64(&c.stats.totalStreams)),
		ActiveStreams:        int(atomic.LoadInt64(&c.stats.activeStreams)),
		FailedStreams:        atomic.LoadUint64(&c.stats.failedStreams),
		BytesSent:            atomic.LoadUint64(&c.stats.bytesSent),
		BytesReceived:        atomic.LoadUint64(&c.stats.bytesReceived),
		AverageRTT:           avgRTT,
		ConnectionsPerSecond: connectionsPerSecond,
	}
}

// Close closes all connections and releases resources.
func (c *client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if err := c.manager.Close(); err != nil {
		return err
	}

	return c.transport.Close()
}

// trackedConn wraps a mux.Stream to track statistics.
type trackedConn struct {
	mux.Stream
	client *client
	closed bool
}

func (tc *trackedConn) Read(b []byte) (n int, err error) {
	n, err = tc.Stream.Read(b)
	if n > 0 {
		atomic.AddUint64(&tc.client.stats.bytesReceived, uint64(n))
	}
	return n, err
}

func (tc *trackedConn) Write(b []byte) (n int, err error) {
	n, err = tc.Stream.Write(b)
	if n > 0 {
		atomic.AddUint64(&tc.client.stats.bytesSent, uint64(n))
	}
	return n, err
}

func (tc *trackedConn) Close() error {
	if !tc.closed {
		tc.closed = true
		atomic.AddInt64(&tc.client.stats.activeStreams, -1)
	}
	return tc.Stream.Close()
}

// validateClientConfig validates and sets defaults for client configuration.
func validateClientConfig(config *ClientConfig) error {
	if config.Certificate.Certificate == nil {
		return fmt.Errorf("certificate is required")
	}

	// Validate certificate has at least one certificate
	if len(config.Certificate.Certificate) == 0 {
		return fmt.Errorf("certificate must contain at least one certificate")
	}

	// Validate device ID if provided
	if config.DeviceID != (DeviceID{}) {
		// Check if device ID matches certificate
		derivedID := protocol.NewDeviceID(config.Certificate.Certificate[0])
		if config.DeviceID != derivedID {
			return fmt.Errorf("provided device ID does not match certificate")
		}
	}

	// Validate and set defaults for numeric values
	if config.MaxConnections < 0 {
		return fmt.Errorf("max connections cannot be negative")
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = DefaultMaxConnections
	}
	if config.MaxConnections > 1000 {
		return fmt.Errorf("max connections cannot exceed 1000")
	}

	if config.MaxStreamsPerConnection < 0 {
		return fmt.Errorf("max streams per connection cannot be negative")
	}
	if config.MaxStreamsPerConnection == 0 {
		config.MaxStreamsPerConnection = DefaultMaxStreamsPerConnection
	}
	if config.MaxStreamsPerConnection > 10000 {
		return fmt.Errorf("max streams per connection cannot exceed 10000")
	}

	// Validate and set defaults for timeouts
	if config.ConnectionTimeout < 0 {
		return fmt.Errorf("connection timeout cannot be negative")
	}
	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = DefaultConnectionTimeout
	}
	if config.ConnectionTimeout > 5*time.Minute {
		return fmt.Errorf("connection timeout cannot exceed 5 minutes")
	}

	if config.IdleTimeout < 0 {
		return fmt.Errorf("idle timeout cannot be negative")
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = DefaultIdleTimeout
	}
	if config.IdleTimeout > 1*time.Hour {
		return fmt.Errorf("idle timeout cannot exceed 1 hour")
	}

	if config.KeepAliveInterval < 0 {
		return fmt.Errorf("keep alive interval cannot be negative")
	}
	if config.KeepAliveInterval == 0 {
		config.KeepAliveInterval = DefaultKeepAliveInterval
	}
	if config.KeepAliveInterval > 10*time.Minute {
		return fmt.Errorf("keep alive interval cannot exceed 10 minutes")
	}

	// Validate country code if provided
	if config.Country != "" && len(config.Country) != 2 {
		return fmt.Errorf("country code must be a 2-letter ISO code")
	}

	return nil
}

// createTransport creates a transport instance with the given configuration.
func createTransport(cert tls.Certificate, country string) transport.Transport {
	// Create relay transport configuration
	config := relay.Config{
		Config: transport.DefaultConfig(),
	}

	// For now, create a relay transport
	// TODO: Add support for multiple transport types and auto-selection
	return relay.NewTransport(cert, config)
}

// Common errors
var (
	ErrClientClosed = fmt.Errorf("client is closed")
	ErrServerClosed = fmt.Errorf("server is closed")
	ErrProxyClosed  = fmt.Errorf("proxy is closed")
)
