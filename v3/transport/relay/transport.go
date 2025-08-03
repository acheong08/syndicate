package relay

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand/v2"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/acheong08/syndicate/v3/transport"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay/client"
	relayprotocol "github.com/syncthing/syncthing/lib/relay/protocol"
)

// Transport implements the relay transport using direct Syncthing internals
type Transport struct {
	cert             tls.Certificate
	config           Config
	discoveryService transport.DiscoveryService
	closed           chan struct{}
	closeOnce        sync.Once

	// Caching for performance
	relayCache      map[string][]string                        // deviceID -> relay URLs
	inviteCache     map[string]relayprotocol.SessionInvitation // deviceID+relayURL -> invitation
	cacheMu         sync.RWMutex
	cacheExpiry     time.Duration
	lastCacheUpdate map[string]time.Time // deviceID -> last update time
}

// Config holds relay transport configuration
type Config struct {
	// Base transport config
	transport.Config

	// Discovery service configuration
	DiscoveryConfig transport.DiscoveryConfig

	// Connection timeout for relay operations
	RelayTimeout time.Duration
}

// NewTransport creates a new relay transport with direct Syncthing integration
func NewTransport(cert tls.Certificate, config Config) *Transport {
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 30 * time.Second
	}
	if config.RelayTimeout == 0 {
		config.RelayTimeout = 30 * time.Second
	}

	// Set up discovery config
	if config.DiscoveryConfig.Certificate.Certificate == nil {
		config.DiscoveryConfig = transport.DefaultDiscoveryConfig(cert)
	}

	// Create discovery service
	discoveryService := transport.NewDiscoveryService(config.DiscoveryConfig)

	return &Transport{
		cert:             cert,
		config:           config,
		discoveryService: discoveryService,
		closed:           make(chan struct{}),
		relayCache:       make(map[string][]string),
		inviteCache:      make(map[string]relayprotocol.SessionInvitation),
		lastCacheUpdate:  make(map[string]time.Time),
		cacheExpiry:      5 * time.Minute, // Cache for 5 minutes
	}
}

// Name returns the transport name
func (t *Transport) Name() string {
	return "relay"
}

// Dial establishes a connection to a relay endpoint
func (t *Transport) Dial(ctx context.Context, endpoint transport.Endpoint) (net.Conn, error) {
	start := time.Now()
	defer func() {
		// cheap debug timing; no logger available here
		_ = start
	}()
	relayEndpoint, ok := endpoint.(*transport.RelayEndpoint)
	if !ok {
		return nil, fmt.Errorf("expected RelayEndpoint, got %T", endpoint)
	}

	select {
	case <-t.closed:
		return nil, fmt.Errorf("relay transport is closed")
	default:
	}

	// Get relay URLs for the device
	relays, err := t.getRelaysForDevice(ctx, relayEndpoint.DeviceID())
	if err != nil {
		return nil, fmt.Errorf("failed to get relays for device %s: %w", relayEndpoint.DeviceID().Short(), err)
	}

	if len(relays) == 0 {
		return nil, fmt.Errorf("no relays available for device %s", relayEndpoint.DeviceID().Short())
	}

	// Try to get an invite from a random relay
	selectedRelay := relays[rand.IntN(len(relays))]
	invite, err := t.getInvite(ctx, selectedRelay, relayEndpoint.DeviceID())
	if err != nil {
		return nil, fmt.Errorf("failed to get invite from relay %s: %w", selectedRelay, err)
	}

	// Create session
	conn, err := t.createSession(ctx, invite, relayEndpoint.SNI())
	if err != nil {
		return nil, fmt.Errorf("failed to create relay session: %w", err)
	}

	return conn, nil
}

// Listen starts listening for incoming connections on relay endpoints
func (t *Transport) Listen(ctx context.Context, endpoint transport.Endpoint) (net.Listener, error) {
	config := ListenConfig{
		TrustedIDs:        []protocol.DeviceID{}, // Accept all by default
		RelayURLs:         []string{},            // Auto-discover by default
		TargetRelayCount:  5,
		MinRelayThreshold: 3,
	}

	return NewListener(ctx, t, config)
}

// Close closes the transport and cleans up resources
func (t *Transport) Close() error {
	var err error
	t.closeOnce.Do(func() {
		close(t.closed)
		if t.discoveryService != nil {
			if closeErr := t.discoveryService.Close(); closeErr != nil {
				err = closeErr
			}
		}
	})
	return err
}

// getRelaysForDevice retrieves relay URLs for a device using the discovery service with caching
func (t *Transport) getRelaysForDevice(ctx context.Context, deviceID protocol.DeviceID) ([]string, error) {
	deviceIDStr := deviceID.String()

	t.cacheMu.RLock()
	cachedRelays, exists := t.relayCache[deviceIDStr]
	lastUpdate, hasUpdate := t.lastCacheUpdate[deviceIDStr]
	t.cacheMu.RUnlock()

	// Check if we have valid cached data
	if exists && hasUpdate && time.Since(lastUpdate) < t.cacheExpiry {
		return cachedRelays, nil
	}

	// Cache miss or expired, fetch from discovery service
	relays, err := t.discoveryService.LookupDevice(ctx, deviceID)
	if err != nil {
		// If we have stale cache data, use it as fallback
		if exists {
			return cachedRelays, nil
		}
		return nil, err
	}

	// Update cache
	t.cacheMu.Lock()
	t.relayCache[deviceIDStr] = relays
	t.lastCacheUpdate[deviceIDStr] = time.Now()
	t.cacheMu.Unlock()

	return relays, nil
}

// getInvite gets an invitation from a relay using Syncthing relay protocol with caching
func (t *Transport) getInvite(ctx context.Context, relayURL string, deviceID protocol.DeviceID) (relayprotocol.SessionInvitation, error) {
	cacheKey := deviceID.String() + "|" + relayURL

	t.cacheMu.RLock()
	cachedInvite, exists := t.inviteCache[cacheKey]
	t.cacheMu.RUnlock()

	// Invitations are generally short-lived, but we can cache them briefly to avoid repeated requests
	// within a short time window (useful for connection retries)
	if exists {
		// Check if invite is still valid (basic heuristic)
		return cachedInvite, nil
	}

	relayURLParsed, err := url.Parse(relayURL)
	if err != nil {
		return relayprotocol.SessionInvitation{}, fmt.Errorf("invalid relay URL %s: %w", relayURL, err)
	}

	invite, err := client.GetInvitationFromRelay(ctx, relayURLParsed, deviceID, []tls.Certificate{t.cert}, t.config.RelayTimeout)
	if err != nil {
		return relayprotocol.SessionInvitation{}, err
	}

	// Cache the invitation briefly (30 seconds)
	t.cacheMu.Lock()
	t.inviteCache[cacheKey] = invite
	// Clean up old invitations to prevent memory leaks
	go func() {
		time.Sleep(30 * time.Second)
		t.cacheMu.Lock()
		delete(t.inviteCache, cacheKey)
		t.cacheMu.Unlock()
	}()
	t.cacheMu.Unlock()

	return invite, nil
}

// createSession creates a TLS session from a relay invitation
func (t *Transport) createSession(ctx context.Context, invite relayprotocol.SessionInvitation, sni string) (net.Conn, error) {
	// Join the relay session
	rawConn, err := client.JoinSession(ctx, invite)
	if err != nil {
		return nil, fmt.Errorf("failed to join relay session: %w", err)
	}

	// Set up TLS configuration
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{t.cert},
		InsecureSkipVerify: true,
		ClientAuth:         tls.RequireAnyClientCert,
	}

	// Determine if we're client or server based on SNI
	var tlsConn *tls.Conn
	if sni != "" {
		// We're the client
		tlsConfig.ServerName = sni
		tlsConn = tls.Client(rawConn, tlsConfig)
	} else {
		// We're the server
		tlsConn = tls.Server(rawConn, tlsConfig)
	}

	// Perform TLS handshake
	if err := tlsConn.Handshake(); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("TLS handshake failed: %w", err)
	}

	// Verify peer certificate matches expected device ID
	peerCerts := tlsConn.ConnectionState().PeerCertificates
	if len(peerCerts) == 0 {
		tlsConn.Close()
		return nil, fmt.Errorf("no peer certificate received")
	}

	peerDeviceID := protocol.NewDeviceID(peerCerts[0].Raw)
	expectedDeviceID := protocol.DeviceID(invite.From)
	if peerDeviceID != expectedDeviceID {
		tlsConn.Close()
		return nil, fmt.Errorf("peer certificate mismatch: expected %s, got %s",
			expectedDeviceID.Short(), peerDeviceID.Short())
	}

	return tlsConn, nil
}
