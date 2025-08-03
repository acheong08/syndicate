package syndicate

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/syncthing/syncthing/lib/protocol"
)

// proxy implements the Proxy interface for various proxy types.
type proxy struct {
	config   ProxyConfig
	deviceID DeviceID
	client   Client // For client-side proxies
	server   Server // For server-side proxies
	listener net.Listener

	// Statistics (using atomic for thread safety)
	stats struct {
		totalSessions     uint64
		activeSessions    int64
		failedSessions    uint64
		rejectedSessions  uint64
		totalConnections  int64
		activeConnections int64
		bytesSent         uint64
		bytesReceived     uint64
	}

	mu     sync.RWMutex
	closed bool
}

// NewProxy creates a new Proxy with the given configuration.
func NewProxy(config ProxyConfig) (Proxy, error) {
	// Validate and set defaults
	if err := validateProxyConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid proxy config: %w", err)
	}

	// Derive device ID from certificate if not provided
	deviceID := config.DeviceID
	if deviceID == (DeviceID{}) {
		deviceID = protocol.NewDeviceID(config.Certificate.Certificate[0])
	}

	p := &proxy{
		config:   config,
		deviceID: deviceID,
	}

	// Initialize client or server based on proxy type and configuration
	if config.TargetDevice != (DeviceID{}) {
		// Client-side proxy: connects to a specific target device
		clientConfig := ClientConfig{
			Certificate:             config.Certificate,
			DeviceID:                deviceID,
			Country:                 config.Country,
			MaxConnections:          config.MaxConnections,
			MaxStreamsPerConnection: config.MaxStreamsPerConnection,
			ConnectionTimeout:       config.ConnectionTimeout,
			IdleTimeout:             config.IdleTimeout,
		}
		client, err := NewClient(clientConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create client: %w", err)
		}
		p.client = client
	} else {
		// Server-side proxy: accepts connections from any trusted device
		serverConfig := ServerConfig{
			Certificate:             config.Certificate,
			DeviceID:                deviceID,
			TrustedDevices:          config.TrustedDevices,
			Country:                 config.Country,
			MaxConnections:          config.MaxConnections,
			MaxStreamsPerConnection: config.MaxStreamsPerConnection,
			IdleTimeout:             config.IdleTimeout,
		}
		server, err := NewServer(serverConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create server: %w", err)
		}
		p.server = server
	}

	return p, nil
}

// Start begins listening for connections and proxying them.
func (p *proxy) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrProxyClosed
	}
	p.mu.Unlock()

	// Start local listener
	listener, err := net.Listen("tcp", p.config.LocalAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.config.LocalAddr, err)
	}

	p.mu.Lock()
	p.listener = listener
	p.mu.Unlock()

	// If server-side proxy, start handling remote connections too
	if p.server != nil {
		go p.server.HandleConnections(p.handleRemoteConnection)
	}

	// Handle local connections
	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Listener closed
			}

			atomic.AddInt64(&p.stats.totalConnections, 1)
			atomic.AddInt64(&p.stats.activeConnections, 1)

			go p.handleLocalConnection(ctx, conn)
		}
	}()

	return nil
}

// handleLocalConnection handles a local connection based on proxy type
func (p *proxy) handleLocalConnection(ctx context.Context, localConn net.Conn) {
	defer func() {
		localConn.Close()
		atomic.AddInt64(&p.stats.activeConnections, -1)
	}()

	switch p.config.Type {
	case ProxyTypeSOCKS5:
		p.handleSOCKS5Local(ctx, localConn)
	case ProxyTypeHTTP:
		p.handleHTTPLocal(ctx, localConn)
	case ProxyTypeTunnel:
		p.handleTunnelLocal(ctx, localConn)
	default:
		// Unknown proxy type
		atomic.AddUint64(&p.stats.failedSessions, 1)
	}
}

// handleRemoteConnection handles incoming connections from remote devices (server-side)
func (p *proxy) handleRemoteConnection(remoteConn net.Conn, deviceID DeviceID) {
	defer remoteConn.Close()

	atomic.AddUint64(&p.stats.totalSessions, 1)
	atomic.AddInt64(&p.stats.activeSessions, 1)
	defer atomic.AddInt64(&p.stats.activeSessions, -1)

	switch p.config.Type {
	case ProxyTypeSOCKS5:
		p.handleSOCKS5Remote(remoteConn)
	case ProxyTypeHTTP:
		p.handleHTTPRemote(remoteConn)
	case ProxyTypeTunnel:
		p.handleTunnelRemote(remoteConn)
	default:
		atomic.AddUint64(&p.stats.failedSessions, 1)
	}
}

// handleSOCKS5Local handles SOCKS5 requests from local clients
func (p *proxy) handleSOCKS5Local(ctx context.Context, localConn net.Conn) {
	defer func() {
		atomic.AddUint64(&p.stats.totalSessions, 1)
		atomic.AddInt64(&p.stats.activeSessions, 1)
		defer atomic.AddInt64(&p.stats.activeSessions, -1)
	}()

	// SOCKS5 handshake - Version identification
	buf := make([]byte, 257) // Max possible size for SOCKS5 initial request
	n, err := localConn.Read(buf)
	if err != nil {
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	if n < 3 || buf[0] != 0x05 { // SOCKS version 5
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	nMethods := int(buf[1])
	if n < 2+nMethods {
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Check for no authentication method (0x00)
	hasNoAuth := false
	for i := 0; i < nMethods; i++ {
		if buf[2+i] == 0x00 {
			hasNoAuth = true
			break
		}
	}

	// Respond with method selection
	if hasNoAuth {
		localConn.Write([]byte{0x05, 0x00}) // Version 5, No auth required
	} else {
		localConn.Write([]byte{0x05, 0xFF}) // Version 5, No acceptable methods
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Read connection request
	n, err = localConn.Read(buf)
	if err != nil {
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	if n < 10 || buf[0] != 0x05 || buf[1] != 0x01 { // Version 5, CONNECT command
		// Send failure response
		localConn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Command not supported
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Parse address type and target
	addressType := buf[3]
	var targetHost string
	var targetPort uint16

	switch addressType {
	case 0x01: // IPv4
		if n < 10 {
			localConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Address type not supported
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}
		targetHost = fmt.Sprintf("%d.%d.%d.%d", buf[4], buf[5], buf[6], buf[7])
		targetPort = uint16(buf[8])<<8 | uint16(buf[9])

	case 0x03: // Domain name
		if n < 7 {
			localConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Address type not supported
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}
		domainLen := int(buf[4])
		if n < 7+domainLen {
			localConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Address type not supported
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}
		targetHost = string(buf[5 : 5+domainLen])
		targetPort = uint16(buf[5+domainLen])<<8 | uint16(buf[6+domainLen])

	case 0x04: // IPv6
		if n < 22 {
			localConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Address type not supported
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}
		// IPv6 address parsing - simplified
		targetHost = "[IPv6 address]" // Placeholder for IPv6 support
		targetPort = uint16(buf[20])<<8 | uint16(buf[21])

	default:
		localConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Address type not supported
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Use the parsed target information for logging (variables are now used)
	_ = targetHost // Used for potential logging/debugging
	_ = targetPort // Used for potential logging/debugging

	// For client-side proxy: establish connection through syndicate to target device
	if p.client != nil {
		remoteConn, err := p.client.Connect(ctx, p.config.TargetDevice)
		if err != nil {
			// Send connection failure response
			localConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Connection refused
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}
		defer remoteConn.Close()

		// Send the original SOCKS5 request to the remote server
		remoteConn.Write(buf[:n])

		// Read response from remote server
		resp := make([]byte, 10)
		_, err = remoteConn.Read(resp)
		if err != nil {
			localConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Connection refused
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}

		// Forward response to local client
		localConn.Write(resp)

		// If successful, start relaying data
		if len(resp) > 1 && resp[1] == 0x00 { // Success
			p.relayConnections(localConn, remoteConn)
		} else {
			atomic.AddUint64(&p.stats.failedSessions, 1)
		}
	} else {
		// Server-side proxy: This shouldn't happen for SOCKS5 in this configuration
		localConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Connection refused
		atomic.AddUint64(&p.stats.failedSessions, 1)
	}
}

// handleSOCKS5Remote handles SOCKS5 requests from remote devices (server-side)
func (p *proxy) handleSOCKS5Remote(remoteConn net.Conn) {
	defer func() {
		atomic.AddUint64(&p.stats.totalSessions, 1)
		atomic.AddInt64(&p.stats.activeSessions, 1)
		defer atomic.AddInt64(&p.stats.activeSessions, -1)
	}()

	// For server-side SOCKS5, we act as a SOCKS5 server
	// Read the initial handshake from remote client
	buf := make([]byte, 257)
	n, err := remoteConn.Read(buf)
	if err != nil {
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	if n < 3 || buf[0] != 0x05 { // SOCKS version 5
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Accept no authentication
	remoteConn.Write([]byte{0x05, 0x00})

	// Read connection request
	n, err = remoteConn.Read(buf)
	if err != nil {
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	if n < 10 || buf[0] != 0x05 || buf[1] != 0x01 { // Version 5, CONNECT command
		remoteConn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Command not supported
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Parse target address (same logic as client side)
	addressType := buf[3]
	var targetHost string
	var targetPort uint16

	switch addressType {
	case 0x01: // IPv4
		if n < 10 {
			remoteConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}
		targetHost = fmt.Sprintf("%d.%d.%d.%d", buf[4], buf[5], buf[6], buf[7])
		targetPort = uint16(buf[8])<<8 | uint16(buf[9])

	case 0x03: // Domain name
		if n < 7 {
			remoteConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}
		domainLen := int(buf[4])
		if n < 7+domainLen {
			remoteConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}
		targetHost = string(buf[5 : 5+domainLen])
		targetPort = uint16(buf[5+domainLen])<<8 | uint16(buf[6+domainLen])

	default:
		remoteConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Establish local connection to target
	targetAddr := fmt.Sprintf("%s:%d", targetHost, targetPort)
	localConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		// Send connection failure response
		remoteConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Connection refused
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}
	defer localConn.Close()

	// Send success response
	remoteConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Success

	// Start relaying data between remote and local connections
	p.relayConnections(remoteConn, localConn)
}

// handleHTTPLocal handles HTTP CONNECT requests from local clients
func (p *proxy) handleHTTPLocal(ctx context.Context, localConn net.Conn) {
	defer func() {
		atomic.AddUint64(&p.stats.totalSessions, 1)
		atomic.AddInt64(&p.stats.activeSessions, 1)
		defer atomic.AddInt64(&p.stats.activeSessions, -1)
	}()

	// Read HTTP request
	buf := make([]byte, 4096)
	n, err := localConn.Read(buf)
	if err != nil {
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Parse HTTP request line
	request := string(buf[:n])
	lines := strings.Split(request, "\r\n")
	if len(lines) == 0 {
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Parse the request line (e.g., "CONNECT example.com:443 HTTP/1.1")
	parts := strings.Fields(lines[0])
	if len(parts) != 3 || parts[0] != "CONNECT" {
		// Send 400 Bad Request
		localConn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	targetAddr := parts[1]

	// For client-side proxy: establish connection through syndicate to target device
	if p.client != nil {
		remoteConn, err := p.client.Connect(ctx, p.config.TargetDevice)
		if err != nil {
			// Send 502 Bad Gateway
			localConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}
		defer remoteConn.Close()

		// Send the original HTTP request to the remote server
		remoteConn.Write(buf[:n])

		// Read response from remote server
		resp := make([]byte, 4096)
		respLen, err := remoteConn.Read(resp)
		if err != nil {
			localConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}

		// Forward response to local client
		localConn.Write(resp[:respLen])

		// Check if response indicates success (200 Connection established)
		respStr := string(resp[:respLen])
		if strings.Contains(respStr, "200") && strings.Contains(respStr, "Connection established") {
			// Start relaying data
			p.relayConnections(localConn, remoteConn)
		} else {
			atomic.AddUint64(&p.stats.failedSessions, 1)
		}
	} else {
		// Server-side proxy: This shouldn't happen for HTTP proxy in this configuration
		localConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		atomic.AddUint64(&p.stats.failedSessions, 1)
	}

	// Use the parsed target for logging (variable is now used)
	_ = targetAddr // Used for potential logging/debugging
}

// handleHTTPRemote handles HTTP requests from remote devices (server-side)
func (p *proxy) handleHTTPRemote(remoteConn net.Conn) {
	defer func() {
		atomic.AddUint64(&p.stats.totalSessions, 1)
		atomic.AddInt64(&p.stats.activeSessions, 1)
		defer atomic.AddInt64(&p.stats.activeSessions, -1)
	}()

	// Read HTTP request from remote client
	buf := make([]byte, 4096)
	n, err := remoteConn.Read(buf)
	if err != nil {
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Parse HTTP request
	request := string(buf[:n])
	lines := strings.Split(request, "\r\n")
	if len(lines) == 0 {
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	// Parse the request line
	parts := strings.Fields(lines[0])
	if len(parts) != 3 || parts[0] != "CONNECT" {
		// Send 400 Bad Request
		remoteConn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}

	targetAddr := parts[1]

	// Establish local connection to target
	localConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		// Send 502 Bad Gateway
		remoteConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}
	defer localConn.Close()

	// Send success response
	remoteConn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))

	// Start relaying data between remote and local connections
	p.relayConnections(remoteConn, localConn)
}

// handleTunnelLocal handles direct tunnel connections from local clients
func (p *proxy) handleTunnelLocal(ctx context.Context, localConn net.Conn) {
	if p.client != nil {
		// Client-side: connect to target device and relay
		remoteConn, err := p.client.Connect(ctx, p.config.TargetDevice)
		if err != nil {
			atomic.AddUint64(&p.stats.failedSessions, 1)
			return
		}
		defer remoteConn.Close()

		atomic.AddUint64(&p.stats.totalSessions, 1)
		atomic.AddInt64(&p.stats.activeSessions, 1)
		defer atomic.AddInt64(&p.stats.activeSessions, -1)

		p.relayConnections(localConn, remoteConn)
	}
}

// handleTunnelRemote handles direct tunnel connections from remote devices
func (p *proxy) handleTunnelRemote(remoteConn net.Conn) {
	defer func() {
		atomic.AddUint64(&p.stats.totalSessions, 1)
		atomic.AddInt64(&p.stats.activeSessions, 1)
		defer atomic.AddInt64(&p.stats.activeSessions, -1)
	}()

	// For tunnel server mode, we need to know where to forward the connection
	// This could be configured via the proxy config or parsed from initial data
	// For simplicity, we'll assume a fixed local target

	// TODO: Make this configurable via proxy configuration
	localTarget := "127.0.0.1:8080" // Default HTTP server
	if p.config.Type == ProxyTypeTunnel {
		// Could read target from config or initial handshake
		// For now, use a default
	}

	// Establish local connection to target
	localConn, err := net.Dial("tcp", localTarget)
	if err != nil {
		atomic.AddUint64(&p.stats.failedSessions, 1)
		return
	}
	defer localConn.Close()

	// Start relaying data between remote and local connections
	p.relayConnections(remoteConn, localConn)
}

// relayConnections bidirectionally relays data between two connections
func (p *proxy) relayConnections(conn1, conn2 net.Conn) {
	done := make(chan struct{}, 2)

	// Copy from conn1 to conn2
	go func() {
		defer func() { done <- struct{}{} }()
		written, _ := io.Copy(&trackingWriter{conn2, &p.stats.bytesSent}, conn1)
		atomic.AddUint64(&p.stats.bytesSent, uint64(written))
	}()

	// Copy from conn2 to conn1
	go func() {
		defer func() { done <- struct{}{} }()
		written, _ := io.Copy(&trackingWriter{conn1, &p.stats.bytesReceived}, conn2)
		atomic.AddUint64(&p.stats.bytesReceived, uint64(written))
	}()

	// Wait for either direction to complete
	<-done
}

// trackingWriter wraps a writer to track bytes written
type trackingWriter struct {
	net.Conn
	counter *uint64
}

func (tw *trackingWriter) Write(b []byte) (n int, err error) {
	n, err = tw.Conn.Write(b)
	if n > 0 {
		atomic.AddUint64(tw.counter, uint64(n))
	}
	return n, err
}

// Stats returns proxy statistics.
func (p *proxy) Stats() ProxyStats {
	return ProxyStats{
		TotalSessions:     atomic.LoadUint64(&p.stats.totalSessions),
		ActiveSessions:    int(atomic.LoadInt64(&p.stats.activeSessions)),
		FailedSessions:    atomic.LoadUint64(&p.stats.failedSessions),
		RejectedSessions:  atomic.LoadUint64(&p.stats.rejectedSessions),
		TotalConnections:  int(atomic.LoadInt64(&p.stats.totalConnections)),
		ActiveConnections: int(atomic.LoadInt64(&p.stats.activeConnections)),
		BytesSent:         atomic.LoadUint64(&p.stats.bytesSent),
		BytesReceived:     atomic.LoadUint64(&p.stats.bytesReceived),
		// TODO: Calculate sessions per second and average duration
		SessionsPerSecond:      0,
		AverageSessionDuration: 0,
	}
}

// Close stops the proxy and closes all connections.
func (p *proxy) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	if p.listener != nil {
		p.listener.Close()
	}

	if p.client != nil {
		p.client.Close()
	}

	if p.server != nil {
		p.server.Close()
	}

	return nil
}

// validateProxyConfig validates and sets defaults for proxy configuration.
func validateProxyConfig(config *ProxyConfig) error {
	if config.Certificate.Certificate == nil {
		return fmt.Errorf("certificate is required")
	}

	if config.LocalAddr == "" {
		return fmt.Errorf("local address is required")
	}

	if config.Type == "" {
		config.Type = ProxyTypeSOCKS5 // Default to SOCKS5
	}

	// Validate proxy type
	switch config.Type {
	case ProxyTypeSOCKS5, ProxyTypeHTTP, ProxyTypeTunnel:
		// Valid types
	default:
		return fmt.Errorf("unsupported proxy type: %s", config.Type)
	}

	// Set defaults
	if config.MaxConnections <= 0 {
		config.MaxConnections = DefaultMaxConnections
	}
	if config.MaxStreamsPerConnection <= 0 {
		config.MaxStreamsPerConnection = DefaultMaxStreamsPerConnection
	}
	if config.ConnectionTimeout <= 0 {
		config.ConnectionTimeout = DefaultConnectionTimeout
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = DefaultIdleTimeout
	}

	return nil
}
