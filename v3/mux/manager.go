package mux

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/acheong08/syndicate/v3/transport"
)

// Manager provides high-level connection management for multiplexed connections
type Manager struct {
	config       *Config
	transport    transport.Transport
	connections  map[string]*Connection
	connMu       sync.RWMutex
	dialTimeout  time.Duration
	cleanupTimer *time.Timer
	closed       bool
	closeMu      sync.RWMutex
}

// Connection represents a managed multiplexed connection
type Connection struct {
	mux      Multiplexer
	addr     string
	lastUsed time.Time
	refCount int32
	created  time.Time
	mu       sync.RWMutex
}

// NewManager creates a new connection manager with transport support
func NewManager(config *Config, transport transport.Transport) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	m := &Manager{
		config:      config,
		transport:   transport,
		connections: make(map[string]*Connection),
		dialTimeout: 30 * time.Second,
	}

	// Start cleanup goroutine
	m.startCleanup()

	return m
}

// DialEndpoint creates or reuses a multiplexed connection to the target endpoint
func (m *Manager) DialEndpoint(ctx context.Context, endpoint transport.Endpoint) (Stream, error) {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return nil, fmt.Errorf("manager is closed")
	}
	m.closeMu.RUnlock()

	conn, err := m.getOrCreateConnectionForEndpoint(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	// Open stream on the connection
	stream, err := conn.mux.OpenStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}

	// Update connection usage
	conn.mu.Lock()
	conn.lastUsed = time.Now()
	conn.refCount++
	conn.mu.Unlock()

	return &managedStream{
		Stream: stream,
		conn:   conn,
	}, nil
}

// getOrCreateConnectionForEndpoint gets an existing connection or creates a new one using transport
func (m *Manager) getOrCreateConnectionForEndpoint(ctx context.Context, endpoint transport.Endpoint) (*Connection, error) {
	address := endpoint.Address()

	// Check for existing connection
	m.connMu.RLock()
	if conn, exists := m.connections[address]; exists && !conn.mux.IsClosed() {
		m.connMu.RUnlock()
		return conn, nil
	}
	m.connMu.RUnlock()

	// Create new connection
	m.connMu.Lock()
	defer m.connMu.Unlock()

	// Double-check after acquiring write lock
	if conn, exists := m.connections[address]; exists && !conn.mux.IsClosed() {
		return conn, nil
	}

	// Dial using transport with timeout
	dialCtx, cancel := context.WithTimeout(ctx, m.dialTimeout)
	defer cancel()

	rawConn, err := m.transport.Dial(dialCtx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to dial endpoint %s: %w", address, err)
	}

	mux := NewClient(rawConn, m.config)
	conn := &Connection{
		mux:      mux,
		addr:     address,
		lastUsed: time.Now(),
		refCount: 0,
		created:  time.Now(),
	}

	m.connections[address] = conn
	return conn, nil
}

// Listen accepts incoming multiplexed connections
func (m *Manager) Listen(ctx context.Context, conn net.Conn) (<-chan Stream, error) {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return nil, fmt.Errorf("manager is closed")
	}
	m.closeMu.RUnlock()

	mux := NewServer(conn, m.config)
	streamChan := make(chan Stream, 64)

	go func() {
		defer close(streamChan)
		defer mux.Close()

		for {
			stream, err := mux.AcceptStream(ctx)
			if err != nil {
				return
			}

			select {
			case streamChan <- stream:
			case <-ctx.Done():
				stream.Close()
				return
			}
		}
	}()

	return streamChan, nil
}

// Close closes the manager and all connections
func (m *Manager) Close() error {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true

	// Stop cleanup timer
	if m.cleanupTimer != nil {
		m.cleanupTimer.Stop()
	}

	// Close all connections
	m.connMu.Lock()
	for _, conn := range m.connections {
		conn.mux.Close()
	}
	m.connections = make(map[string]*Connection)
	m.connMu.Unlock()

	return nil
}

// Statistics returns aggregated statistics for all connections
func (m *Manager) Statistics() map[string]Statistics {
	m.connMu.RLock()
	defer m.connMu.RUnlock()

	stats := make(map[string]Statistics)
	for addr, conn := range m.connections {
		stats[addr] = conn.mux.Statistics()
	}

	return stats
}

// startCleanup starts the connection cleanup goroutine
func (m *Manager) startCleanup() {
	if m.config.IdleTimeout <= 0 {
		return
	}

	m.cleanupTimer = time.AfterFunc(m.config.IdleTimeout/2, func() {
		m.cleanup()
		if !m.closed {
			m.cleanupTimer.Reset(m.config.IdleTimeout / 2)
		}
	})
}

// cleanup removes idle connections
func (m *Manager) cleanup() {
	m.closeMu.RLock()
	if m.closed {
		m.closeMu.RUnlock()
		return
	}
	m.closeMu.RUnlock()

	m.connMu.Lock()
	defer m.connMu.Unlock()

	now := time.Now()
	for addr, conn := range m.connections {
		conn.mu.RLock()
		isIdle := now.Sub(conn.lastUsed) > m.config.IdleTimeout
		refCount := conn.refCount
		// Keep connections alive longer for better persistence (up to 10 minutes)
		maxAge := 10 * time.Minute
		isOld := now.Sub(conn.created) > maxAge
		conn.mu.RUnlock()

		// Only remove if connection is both idle, has no references, AND is old
		if ((isIdle && refCount == 0) || conn.mux.IsClosed()) && isOld {
			conn.mux.Close()
			delete(m.connections, addr)
		}
	}
}

// managedStream wraps a stream with connection reference counting
type managedStream struct {
	Stream
	conn   *Connection
	closed bool
	mu     sync.Mutex
}

// Close closes the stream and decreases connection reference count
func (ms *managedStream) Close() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.closed {
		return nil
	}
	ms.closed = true

	// Decrease reference count
	ms.conn.mu.Lock()
	ms.conn.refCount--
	ms.conn.mu.Unlock()

	return ms.Stream.Close()
}

// DialFunc is a function that can dial connections
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// NewManagerWithDialer creates a manager with a custom dialer
func NewManagerWithDialer(config *Config, dialer DialFunc) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	m := &managerWithDialer{
		Manager: Manager{
			config:      config,
			connections: make(map[string]*Connection),
			dialTimeout: 30 * time.Second,
		},
		dialer: dialer,
	}

	m.startCleanup()
	return &m.Manager
}

// managerWithDialer extends Manager with custom dialing
type managerWithDialer struct {
	Manager
	dialer DialFunc
}

// getOrCreateConnection overrides the default implementation
func (m *managerWithDialer) getOrCreateConnection(ctx context.Context, network, address string) (*Connection, error) {
	// Check for existing connection
	m.connMu.RLock()
	if conn, exists := m.connections[address]; exists && !conn.mux.IsClosed() {
		m.connMu.RUnlock()
		return conn, nil
	}
	m.connMu.RUnlock()

	// Create new connection
	m.connMu.Lock()
	defer m.connMu.Unlock()

	// Double-check after acquiring write lock
	if conn, exists := m.connections[address]; exists && !conn.mux.IsClosed() {
		return conn, nil
	}

	// Dial with custom dialer
	dialCtx, cancel := context.WithTimeout(ctx, m.dialTimeout)
	defer cancel()

	rawConn, err := m.dialer(dialCtx, network, address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s://%s: %w", network, address, err)
	}

	mux := NewClient(rawConn, m.config)
	conn := &Connection{
		mux:      mux,
		addr:     address,
		lastUsed: time.Now(),
		refCount: 0,
		created:  time.Now(),
	}

	m.connections[address] = conn
	return conn, nil
}
