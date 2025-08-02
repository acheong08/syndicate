package mux

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

// TunnelManager manages multiplexed connections to syncthing devices
type TunnelManager struct {
	cert    tls.Certificate
	pools   map[protocol.DeviceID]*ConnectionPool
	poolsMu sync.RWMutex
	dialer  interface {
		Dial(ctx context.Context, network, addr string) (net.Conn, error)
	}

	// Configuration
	maxPoolSize    int
	maxIdleTime    time.Duration
	connectTimeout time.Duration
}

// ConnectionPool manages multiple sessions to a single device
type ConnectionPool struct {
	deviceID   protocol.DeviceID
	sessions   []*Session
	sessionsMu sync.RWMutex
	lastUsed   time.Time
	dialer     interface {
		Dial(ctx context.Context, network, addr string) (net.Conn, error)
	}

	// Configuration
	maxSessions int
	maxIdleTime time.Duration
}

// TunnelManagerOption configures the TunnelManager
type TunnelManagerOption func(*TunnelManager)

// WithMaxPoolSize sets the maximum number of sessions per device
func WithMaxPoolSize(size int) TunnelManagerOption {
	return func(tm *TunnelManager) {
		tm.maxPoolSize = size
	}
}

// WithMaxIdleTime sets how long idle connections are kept
func WithMaxIdleTime(duration time.Duration) TunnelManagerOption {
	return func(tm *TunnelManager) {
		tm.maxIdleTime = duration
	}
}

// WithConnectTimeout sets the connection establishment timeout
func WithConnectTimeout(timeout time.Duration) TunnelManagerOption {
	return func(tm *TunnelManager) {
		tm.connectTimeout = timeout
	}
}

// NewTunnelManager creates a new tunnel manager
func NewTunnelManager(cert tls.Certificate, dialer interface {
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
}, opts ...TunnelManagerOption) *TunnelManager {
	tm := &TunnelManager{
		cert:           cert,
		pools:          make(map[protocol.DeviceID]*ConnectionPool),
		dialer:         dialer,
		maxPoolSize:    3,
		maxIdleTime:    5 * time.Minute,
		connectTimeout: 30 * time.Second,
	}

	for _, opt := range opts {
		opt(tm)
	}

	// Start cleanup goroutine
	go tm.cleanup()

	return tm
}

// GetConnection gets a virtual connection to the specified device
func (tm *TunnelManager) GetConnection(ctx context.Context, deviceID protocol.DeviceID) (net.Conn, error) {
	pool := tm.getOrCreatePool(deviceID)
	return pool.getConnection(ctx)
}

// AcceptConnections accepts incoming multiplexed connections
func (tm *TunnelManager) AcceptConnections(ctx context.Context, conn net.Conn) (<-chan net.Conn, error) {
	session := NewServerSession(ctx, conn)
	connChan := make(chan net.Conn, 16)

	go func() {
		defer close(connChan)
		defer session.Close()

		for {
			vc, err := session.AcceptStream()
			if err != nil {
				return
			}

			select {
			case connChan <- vc:
			case <-ctx.Done():
				vc.Close()
				return
			}
		}
	}()

	return connChan, nil
}

// Close closes the tunnel manager and all connections
func (tm *TunnelManager) Close() error {
	tm.poolsMu.Lock()
	defer tm.poolsMu.Unlock()

	for _, pool := range tm.pools {
		pool.close()
	}
	tm.pools = make(map[protocol.DeviceID]*ConnectionPool)

	return nil
}

// getOrCreatePool gets or creates a connection pool for the device
func (tm *TunnelManager) getOrCreatePool(deviceID protocol.DeviceID) *ConnectionPool {
	tm.poolsMu.RLock()
	pool, exists := tm.pools[deviceID]
	tm.poolsMu.RUnlock()

	if exists {
		return pool
	}

	tm.poolsMu.Lock()
	defer tm.poolsMu.Unlock()

	// Double-check after acquiring write lock
	if pool, exists := tm.pools[deviceID]; exists {
		return pool
	}

	pool = &ConnectionPool{
		deviceID:    deviceID,
		sessions:    make([]*Session, 0, tm.maxPoolSize),
		dialer:      tm.dialer,
		maxSessions: tm.maxPoolSize,
		maxIdleTime: tm.maxIdleTime,
		lastUsed:    time.Now(),
	}

	tm.pools[deviceID] = pool
	return pool
}

// cleanup periodically cleans up idle connections
func (tm *TunnelManager) cleanup() {
	ticker := time.NewTicker(tm.maxIdleTime / 2)
	defer ticker.Stop()

	for range ticker.C {
		tm.poolsMu.Lock()
		for deviceID, pool := range tm.pools {
			if time.Since(pool.lastUsed) > tm.maxIdleTime {
				pool.close()
				delete(tm.pools, deviceID)
			} else {
				pool.cleanup()
			}
		}
		tm.poolsMu.Unlock()
	}
}

// getConnection gets a connection from the pool
func (cp *ConnectionPool) getConnection(ctx context.Context) (net.Conn, error) {
	cp.sessionsMu.Lock()
	defer cp.sessionsMu.Unlock()

	cp.lastUsed = time.Now()

	// Try to find an existing session with capacity
	for _, session := range cp.sessions {
		select {
		case <-session.closed:
			// Session is closed, skip
			continue
		default:
			// Session is available, use it
			return session.OpenStream()
		}
	}

	// No available sessions, create a new one if under limit
	if len(cp.sessions) < cp.maxSessions {
		session, err := cp.createSession(ctx)
		if err != nil {
			return nil, err
		}

		cp.sessions = append(cp.sessions, session)
		return session.OpenStream()
	}

	// All sessions are at capacity or there's an error, use the first available
	if len(cp.sessions) > 0 {
		return cp.sessions[0].OpenStream()
	}

	return nil, fmt.Errorf("no available sessions")
}

// createSession creates a new session to the device
func (cp *ConnectionPool) createSession(ctx context.Context) (*Session, error) {
	// Use syncthing dialer to connect
	serverAddr := fmt.Sprintf("%s.syncthing:0", cp.deviceID.String())
	conn, err := cp.dialer.Dial(ctx, "tcp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to device %s: %v", cp.deviceID.Short(), err)
	}

	return NewClientSession(ctx, conn), nil
}

// close closes all sessions in the pool
func (cp *ConnectionPool) close() {
	cp.sessionsMu.Lock()
	defer cp.sessionsMu.Unlock()

	for _, session := range cp.sessions {
		session.Close()
	}
	cp.sessions = cp.sessions[:0]
}

// cleanup removes closed sessions from the pool
func (cp *ConnectionPool) cleanup() {
	cp.sessionsMu.Lock()
	defer cp.sessionsMu.Unlock()

	activeSessions := cp.sessions[:0]
	for _, session := range cp.sessions {
		select {
		case <-session.closed:
			// Session is closed, don't keep it
		default:
			// Session is still active
			activeSessions = append(activeSessions, session)
		}
	}
	cp.sessions = activeSessions
}
