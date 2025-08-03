package relay

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/syncthing/syncthing/lib/connections/registry"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/events"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay/client"
	relayprotocol "github.com/syncthing/syncthing/lib/relay/protocol"
)

// Listener implements net.Listener for relay connections using direct Syncthing imports
type Listener struct {
	transport        *Transport
	acceptChan       chan net.Conn
	errorChan        chan error
	closed           chan struct{}
	closeOnce        sync.Once
	trustedIDs       []protocol.DeviceID
	relayURLs        []string
	ctx              context.Context
	cancel           context.CancelFunc
	listeners        []*relayListener
	listenersMu      sync.Mutex
	addressLister    *addressLister
	discoveryStarted bool
	discoveryMu      sync.Mutex
}

type relayListener struct {
	url        string
	inviteChan <-chan relayprotocol.SessionInvitation
	cancel     context.CancelFunc
}

// ListenConfig configures a relay listener
type ListenConfig struct {
	// Trusted device IDs (empty = accept all)
	TrustedIDs []protocol.DeviceID

	// Specific relay URLs to listen on (empty = auto-discover)
	RelayURLs []string

	// Country preference for relay selection
	RelayCountry string

	// Number of relays to maintain
	TargetRelayCount int

	// Minimum number of relays before searching for more
	MinRelayThreshold int
}

// RelayInfo represents relay information from the public API
type RelayInfo struct {
	URL      string   `json:"url"`
	Location Location `json:"location"`
	Stats    Stats    `json:"stats"`
}

type Location struct {
	Country   string  `json:"country"`
	Continent string  `json:"continent"`
	City      string  `json:"city"`
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
}

type Stats struct {
	StartTime         string `json:"startTime"`
	UptimeSeconds     int    `json:"uptimeSeconds"`
	NumActiveSessions int    `json:"numActiveSessions"`
	Options           struct {
		NetworkTimeout int `json:"network-timeout"`
		PingInterval   int `json:"ping-interval"`
		MessageTimeout int `json:"message-timeout"`
		PerSessionRate int `json:"per-session-rate"`
		GlobalRate     int `json:"global-rate"`
	} `json:"options"`
}

type RelayList struct {
	Relays []RelayInfo `json:"relays"`
}

// NewListener creates a relay listener using direct Syncthing imports
func NewListener(ctx context.Context, transport *Transport, config ListenConfig) (*Listener, error) {
	if config.TargetRelayCount == 0 {
		config.TargetRelayCount = 5
	}
	if config.MinRelayThreshold == 0 {
		config.MinRelayThreshold = 3
	}

	ctx, cancel := context.WithCancel(ctx)

	l := &Listener{
		transport:     transport,
		acceptChan:    make(chan net.Conn, 16),
		errorChan:     make(chan error, 1),
		closed:        make(chan struct{}),
		trustedIDs:    config.TrustedIDs,
		relayURLs:     config.RelayURLs,
		ctx:           ctx,
		cancel:        cancel,
		addressLister: newAddressLister(),
	}

	// Start relay management
	go l.manageRelays(config)

	return l, nil
}

// Accept waits for and returns the next connection to the listener
func (l *Listener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.acceptChan:
		return conn, nil
	case err := <-l.errorChan:
		return nil, err
	case <-l.closed:
		return nil, fmt.Errorf("listener closed")
	case <-l.ctx.Done():
		return nil, l.ctx.Err()
	}
}

// Close closes the listener
func (l *Listener) Close() error {
	var err error
	l.closeOnce.Do(func() {
		l.cancel()
		close(l.closed)

		// Close all relay listeners
		l.listenersMu.Lock()
		for _, rl := range l.listeners {
			rl.cancel()
		}
		l.listeners = nil
		l.listenersMu.Unlock()
	})
	return err
}

// Addr returns the listener's network address
func (l *Listener) Addr() net.Addr {
	return &RelayAddr{DeviceID: protocol.NewDeviceID(l.transport.cert.Certificate[0])}
}

// RelayAddr implements net.Addr for relay addresses
type RelayAddr struct {
	DeviceID protocol.DeviceID
}

func (a *RelayAddr) Network() string { return "relay" }
func (a *RelayAddr) String() string  { return fmt.Sprintf("relay:%s", a.DeviceID.Short()) }

// manageRelays manages the pool of relay listeners
func (l *Listener) manageRelays(config ListenConfig) {
	retryDelay := 10 * time.Second

	for {
		select {
		case <-l.ctx.Done():
			return
		default:
		}

		// Check if we need more relays
		l.listenersMu.Lock()
		activeCount := len(l.listeners)
		l.listenersMu.Unlock()

		if activeCount < config.MinRelayThreshold {
			log.Printf("Active relays (%d) below threshold (%d), searching for new relays...",
				activeCount, config.MinRelayThreshold)

			var relaysToUse []string
			if len(l.relayURLs) > 0 {
				// Use configured relay URLs
				relaysToUse = l.relayURLs
			} else {
				// Find optimal relays using direct API call
				relays, err := l.findOptimalRelays(config.RelayCountry, config.TargetRelayCount)
				if err != nil {
					log.Printf("Failed to find relays: %v", err)
					time.Sleep(retryDelay)
					continue
				}
				relaysToUse = relays
			}

			// Start listeners for new relays (address announcement happens in startRelayListener)
			for _, relayURL := range relaysToUse {
				l.startRelayListener(relayURL)
			}
		}

		time.Sleep(retryDelay)
	}
}

// startDiscoveryBroadcast starts broadcasting to Syncthing discovery using the transport's discovery service
func (l *Listener) startDiscoveryBroadcast() {
	// Get discovery config from transport
	discoveryConfig := l.transport.config.DiscoveryConfig
	if len(discoveryConfig.DiscoveryEndpoints) == 0 {
		log.Printf("No discovery endpoints configured, skipping discovery broadcast")
		return
	}

	// Log current addresses before starting broadcast
	addresses := l.addressLister.ExternalAddresses()
	log.Printf("DEBUG: Starting discovery broadcast with addresses: %v", addresses)

	// Use all discovery endpoints for redundant announcing
	for _, endpoint := range discoveryConfig.DiscoveryEndpoints {
		go func(endpoint string) {
			disco, err := discover.NewGlobal(endpoint, l.transport.cert, l.addressLister, events.NoopLogger, registry.New())
			if err != nil {
				log.Printf("Failed to create discovery broadcaster for %s: %v", endpoint, err)
				return
			}

			log.Printf("Starting discovery broadcast to %s", endpoint)
			if err := disco.Serve(l.ctx); err != nil && l.ctx.Err() == nil {
				log.Printf("Discovery broadcast error for %s: %v", endpoint, err)
			}
		}(endpoint)
	}
}

// findOptimalRelays finds optimal relays using direct HTTP API call
func (l *Listener) findOptimalRelays(country string, maxResults int) ([]string, error) {
	// Fetch relay list from Syncthing's public API
	resp, err := http.Get("https://relays.syncthing.net/endpoint/full")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch relay list: %w", err)
	}
	defer resp.Body.Close()

	var relayList RelayList
	if err := json.NewDecoder(resp.Body).Decode(&relayList); err != nil {
		return nil, fmt.Errorf("failed to decode relay list: %w", err)
	}

	// Filter by country if specified
	var filteredRelays []RelayInfo
	for _, relay := range relayList.Relays {
		if country == "" || relay.Location.Country == country {
			filteredRelays = append(filteredRelays, relay)
		}
	}

	if len(filteredRelays) == 0 && country != "" {
		// Fallback to all relays if country filter yields no results
		log.Printf("No relays found for country '%s', using all available relays", country)
		filteredRelays = relayList.Relays
	}

	// Sort by quality heuristic
	slices.SortFunc(filteredRelays, func(a, b RelayInfo) int {
		scoreA := l.calculateRelayScore(a)
		scoreB := l.calculateRelayScore(b)
		return scoreB - scoreA // Higher score first
	})

	// Test connectivity and return working relays
	return l.testRelayConnectivity(filteredRelays, maxResults)
}

// calculateRelayScore calculates a quality score for a relay
func (l *Listener) calculateRelayScore(relay RelayInfo) int {
	score := 0

	// Prefer relays with higher uptime
	score += relay.Stats.UptimeSeconds / 3600 // Convert to hours

	// Prefer relays with fewer active sessions (less loaded)
	if relay.Stats.NumActiveSessions < 100 {
		score += 50
	} else if relay.Stats.NumActiveSessions < 500 {
		score += 25
	}

	// Prefer relays with higher rate limits
	if relay.Stats.Options.GlobalRate > 0 {
		score += relay.Stats.Options.GlobalRate / 1000
	}
	if relay.Stats.Options.PerSessionRate > 0 {
		score += relay.Stats.Options.PerSessionRate / 1000
	}

	return score
}

// testRelayConnectivity tests connectivity to relays and returns working ones
func (l *Listener) testRelayConnectivity(relays []RelayInfo, maxResults int) ([]string, error) {
	results := make(chan string, maxResults)
	var wg sync.WaitGroup

	testLimit := maxResults * 2
	if testLimit > len(relays) {
		testLimit = len(relays)
	}

	// Test connectivity to top relays concurrently
	for i := 0; i < testLimit; i++ {
		wg.Add(1)
		go func(relay RelayInfo) {
			defer wg.Done()

			relayURL, err := url.Parse(relay.URL)
			if err != nil {
				return
			}

			// Test TCP connectivity
			timeout := 5 * time.Second
			conn, err := net.DialTimeout("tcp", relayURL.Host, timeout)
			if err == nil {
				conn.Close()
				select {
				case results <- relay.URL:
				case <-l.ctx.Done():
				}
			}
		}(relays[i])
	}

	// Close results channel when all tests complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect working relays
	var workingRelays []string
	for relayURL := range results {
		workingRelays = append(workingRelays, relayURL)
		if len(workingRelays) >= maxResults {
			break
		}
	}

	if len(workingRelays) == 0 {
		return nil, fmt.Errorf("no working relays found")
	}

	return workingRelays, nil
}

// startRelayListener starts listening on a specific relay
func (l *Listener) startRelayListener(relayURL string) {
	l.listenersMu.Lock()
	defer l.listenersMu.Unlock()

	// Check if already listening on this relay
	for _, rl := range l.listeners {
		if rl.url == relayURL {
			return
		}
	}

	ctx, cancel := context.WithCancel(l.ctx)

	// Start listening on the relay using direct Syncthing client
	inviteChan, err := l.listenOnRelay(ctx, relayURL)
	if err != nil {
		log.Printf("Failed to listen on relay %s: %v", relayURL, err)
		cancel()
		return
	}

	rl := &relayListener{
		url:        relayURL,
		inviteChan: inviteChan,
		cancel:     cancel,
	}

	l.listeners = append(l.listeners, rl)

	// Update address list with successful relay URLs - this is crucial for discovery broadcasting
	l.updateSuccessfulRelays()

	// Start discovery broadcast if this is the first successful relay connection
	l.discoveryMu.Lock()
	if !l.discoveryStarted {
		l.discoveryStarted = true
		l.discoveryMu.Unlock()
		go l.startDiscoveryBroadcast()
	} else {
		l.discoveryMu.Unlock()
	}

	// Handle invitations from this relay
	go l.handleRelayInvitations(rl)
}

// updateSuccessfulRelays updates the address lister with URLs of successfully connected relays
func (l *Listener) updateSuccessfulRelays() {
	var activeRelayURLs []string
	for _, rl := range l.listeners {
		activeRelayURLs = append(activeRelayURLs, rl.url)
	}
	l.addressLister.updateAddresses(activeRelayURLs)
}

// listenOnRelay starts listening on a relay using direct Syncthing client
func (l *Listener) listenOnRelay(ctx context.Context, relayURL string) (<-chan relayprotocol.SessionInvitation, error) {
	parsedURL, err := url.Parse(relayURL)
	if err != nil {
		return nil, fmt.Errorf("invalid relay URL: %w", err)
	}

	relayClient, err := client.NewClient(parsedURL, []tls.Certificate{l.transport.cert}, l.transport.config.RelayTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create relay client: %w", err)
	}

	go relayClient.Serve(ctx)
	return relayClient.Invitations(), nil
}

// handleRelayInvitations processes invitations from a relay listener
func (l *Listener) handleRelayInvitations(rl *relayListener) {
	defer func() {
		// Remove this listener when done
		l.listenersMu.Lock()
		for i, listener := range l.listeners {
			if listener == rl {
				l.listeners = append(l.listeners[:i], l.listeners[i+1:]...)
				break
			}
		}
		// Update addresses after removing the relay
		l.updateSuccessfulRelays()
		l.listenersMu.Unlock()

		log.Printf("Relay listener disconnected: %s", rl.url)
	}()

	for {
		select {
		case <-l.ctx.Done():
			return
		case invite, ok := <-rl.inviteChan:
			if !ok {
				log.Printf("Invite channel closed for relay %s", rl.url)
				return
			}

			// Check if device is trusted
			if len(l.trustedIDs) > 0 && !l.isTrusted(invite.From) {
				continue
			}

			// Create session using transport's method
			conn, err := l.transport.createSession(l.ctx, invite, "")
			if err != nil {
				log.Printf("Error creating session for relay %s: %v", rl.url, err)
				continue
			}

			// Send connection to accept channel
			select {
			case l.acceptChan <- conn:
			case <-l.ctx.Done():
				conn.Close()
				return
			default:
				// Accept channel is full, close connection
				conn.Close()
				log.Printf("Accept channel full, dropping connection from relay %s", rl.url)
			}
		}
	}
}

// isTrusted checks if a device ID is in the trusted list
func (l *Listener) isTrusted(deviceIDBytes []byte) bool {
	for _, trustedID := range l.trustedIDs {
		if string(deviceIDBytes) == string(trustedID[:]) {
			return true
		}
	}
	return false
}

// addressLister implements the Syncthing address lister interface
type addressLister struct {
	addresses []string
	mu        sync.RWMutex
}

func newAddressLister() *addressLister {
	return &addressLister{}
}

func (a *addressLister) updateAddresses(addresses []string) {
	a.mu.Lock()
	a.addresses = addresses
	a.mu.Unlock()
	log.Printf("DEBUG: Updated addresses for discovery: %v", addresses)
}

func (a *addressLister) ExternalAddresses() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]string(nil), a.addresses...)
}

func (a *addressLister) AllAddresses() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]string(nil), a.addresses...)
}
