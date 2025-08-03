package syndicate

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"github.com/acheong08/syndicate/v3/transport"
	"github.com/syncthing/syncthing/lib/protocol"
)

// server implements the Server interface.
type server struct {
	config            ServerConfig
	deviceID          DeviceID
	transport         transport.Transport
	deviceConnections map[DeviceID]int

	mu     sync.RWMutex
	closed bool
}

// NewServer creates a new Server with the given configuration.
func NewServer(config ServerConfig) (Server, error) {
	// Validate and set defaults
	if err := validateServerConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid server config: %w", err)
	}

	// Create transport
	transport := createTransport(config.Certificate, config.Country)

	// Derive device ID from certificate if not provided
	deviceID := config.DeviceID
	if deviceID == (DeviceID{}) {
		deviceID = protocol.NewDeviceID(config.Certificate.Certificate[0])
	}

	return &server{
		config:            config,
		deviceID:          deviceID,
		transport:         transport,
		deviceConnections: make(map[DeviceID]int),
	}, nil
}

// HandleConnections starts accepting connections and calls the handler for each new virtual connection.
func (s *server) HandleConnections(handler ConnectionHandler) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrServerClosed
	}
	s.mu.Unlock()

	ctx := context.Background()

	// Create endpoint for listening (the transport will handle discovery)
	// For listening, we use our own device ID as the endpoint
	serverDeviceID := protocol.NewDeviceID(s.config.Certificate.Certificate[0])
	endpoint := transport.NewRelayEndpointWithRelays(serverDeviceID, "", nil)

	// Use transport's Listen method (which handles proper discovery and relay management)
	listener, err := s.transport.Listen(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("failed to start transport listener: %w", err)
	}
	defer listener.Close()

	// Accept connections from the transport listener
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// Continue accepting even if individual connections fail
				continue
			}
		}

		// Handle connection in goroutine
		go s.handleConnection(conn, handler)
	}
}

// handleConnection processes a single incoming connection
func (s *server) handleConnection(conn net.Conn, handler ConnectionHandler) {
	defer conn.Close()

	// Get peer device ID from TLS connection state
	var peerDevice DeviceID
	if tlsConn, ok := conn.(*tls.Conn); ok {
		if err := tlsConn.Handshake(); err != nil {
			return
		}

		state := tlsConn.ConnectionState()
		if len(state.PeerCertificates) > 0 {
			peerDevice = protocol.NewDeviceID(state.PeerCertificates[0].Raw)
		}
	}

	// Check if device is trusted
	if len(s.config.TrustedDevices) > 0 {
		trusted := false
		for _, trustedDevice := range s.config.TrustedDevices {
			if peerDevice == trustedDevice {
				trusted = true
				break
			}
		}
		if !trusted {
			return
		}
	}

	// Update connection statistics
	s.mu.Lock()
	s.deviceConnections[peerDevice]++
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.deviceConnections[peerDevice]--
		if s.deviceConnections[peerDevice] <= 0 {
			delete(s.deviceConnections, peerDevice)
		}
		s.mu.Unlock()
	}()

	// Call the connection handler
	handler(conn, peerDevice)
}

// Close closes the server and stops accepting connections.
func (s *server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrServerClosed
	}

	s.closed = true
	return nil
}

// Stats returns current server statistics.
func (s *server) Stats() ServerStats {
	return ServerStats{
		// TODO: Implement statistics collection
	}
}

// validateServerConfig validates and sets defaults for server configuration.
func validateServerConfig(config *ServerConfig) error {
	// Validate certificate
	if len(config.Certificate.Certificate) == 0 {
		return fmt.Errorf("certificate is required")
	}

	// Set defaults
	if config.MaxConnections == 0 {
		config.MaxConnections = 100
	}
	if config.MaxStreamsPerConnection == 0 {
		config.MaxStreamsPerConnection = 10
	}

	// Validate country code if provided
	if config.Country != "" && len(config.Country) != 2 {
		return fmt.Errorf("country code must be a 2-letter ISO code")
	}

	return nil
}
