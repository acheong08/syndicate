package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/events"
	"github.com/syncthing/syncthing/lib/protocol"
)

// DiscoveryService provides device discovery capabilities
type DiscoveryService interface {
	// LookupDevice returns available addresses for a device
	LookupDevice(ctx context.Context, deviceID protocol.DeviceID) ([]string, error)

	// Close cleans up discovery resources
	Close() error
}

// DiscoveryConfig configures device discovery
type DiscoveryConfig struct {
	// Certificate for discovery authentication
	Certificate tls.Certificate

	// Discovery endpoints (empty = auto-detect)
	DiscoveryEndpoints []string

	// Cache TTL for discovery results
	CacheTTL time.Duration

	// Discovery timeout
	Timeout time.Duration
}

// DefaultDiscoveryConfig returns sensible discovery defaults
func DefaultDiscoveryConfig(cert tls.Certificate) DiscoveryConfig {
	return DiscoveryConfig{
		Certificate:        cert,
		DiscoveryEndpoints: getDefaultDiscoveryEndpoints(),
		CacheTTL:           5 * time.Minute,
		Timeout:            30 * time.Second,
	}
}

// discoveryService implements DiscoveryService using Syncthing's discovery
type discoveryService struct {
	cert      tls.Certificate
	config    DiscoveryConfig
	cache     map[protocol.DeviceID]discoveryEntry
	cacheMu   sync.RWMutex
	closed    chan struct{}
	closeOnce sync.Once
}

type discoveryEntry struct {
	addresses []string
	timestamp time.Time
}

// NewDiscoveryService creates a new discovery service
func NewDiscoveryService(config DiscoveryConfig) DiscoveryService {
	return &discoveryService{
		cert:   config.Certificate,
		config: config,
		cache:  make(map[protocol.DeviceID]discoveryEntry),
		closed: make(chan struct{}),
	}
}

// LookupDevice performs device discovery using Syncthing's discovery service
func (d *discoveryService) LookupDevice(ctx context.Context, deviceID protocol.DeviceID) ([]string, error) {
	select {
	case <-d.closed:
		return nil, fmt.Errorf("discovery service is closed")
	default:
	}

	// Check cache first
	d.cacheMu.RLock()
	entry, exists := d.cache[deviceID]
	d.cacheMu.RUnlock()

	if exists && time.Since(entry.timestamp) < d.config.CacheTTL {
		return entry.addresses, nil
	}

	// Cache miss or expired, perform discovery lookup
	addresses, err := d.performLookup(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("device discovery lookup failed: %w", err)
	}

	// Update cache
	d.cacheMu.Lock()
	d.cache[deviceID] = discoveryEntry{
		addresses: addresses,
		timestamp: time.Now(),
	}
	d.cacheMu.Unlock()

	return addresses, nil
}

// performLookup executes the actual discovery lookup
func (d *discoveryService) performLookup(ctx context.Context, deviceID protocol.DeviceID) ([]string, error) {
	// Try each discovery endpoint until we get results
	for _, endpoint := range d.config.DiscoveryEndpoints {
		addresses, err := d.lookupFromEndpoint(ctx, endpoint, deviceID)
		if err == nil && len(addresses) > 0 {
			return addresses, nil
		}
	}

	return nil, fmt.Errorf("no addresses found for device %s", deviceID.Short())
}

// lookupFromEndpoint performs lookup from a specific discovery endpoint
func (d *discoveryService) lookupFromEndpoint(ctx context.Context, endpoint string, deviceID protocol.DeviceID) ([]string, error) {
	// Create discovery client for this endpoint
	disco, err := discover.NewGlobal(endpoint, d.cert, &noopLister{}, events.NoopLogger, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client for %s: %w", endpoint, err)
	}

	// Add timeout to context
	lookupCtx, cancel := context.WithTimeout(ctx, d.config.Timeout)
	defer cancel()

	addresses, err := disco.Lookup(lookupCtx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("discovery lookup failed for endpoint %s: %w", endpoint, err)
	}

	return addresses, nil
}

// Close closes the discovery service and cleans up resources
func (d *discoveryService) Close() error {
	var err error
	d.closeOnce.Do(func() {
		close(d.closed)

		// Clear cache
		d.cacheMu.Lock()
		d.cache = make(map[protocol.DeviceID]discoveryEntry)
		d.cacheMu.Unlock()
	})
	return err
}

// getDefaultDiscoveryEndpoints returns default Syncthing discovery endpoints
func getDefaultDiscoveryEndpoints() []string {
	endpoints := make([]string, 0, len(config.DefaultDiscoveryServersV4)+len(config.DefaultDiscoveryServersV6))

	// Prefer IPv6 if available
	if isIPv6Available() {
		endpoints = append(endpoints, config.DefaultDiscoveryServersV6...)
	}

	// Add IPv4 endpoints
	if isIPv4Available() {
		endpoints = append(endpoints, config.DefaultDiscoveryServersV4...)
	}

	if len(endpoints) == 0 {
		panic("no discovery endpoints available")
	}

	return endpoints
}

// isIPv6Available checks if IPv6 connectivity is available
func isIPv6Available() bool {
	conn, err := net.DialTimeout("tcp6", "[2001:4860:4860::8888]:53", 5*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// isIPv4Available checks if IPv4 connectivity is available
func isIPv4Available() bool {
	conn, err := net.DialTimeout("tcp4", "8.8.8.8:53", 5*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// noopLister implements the address lister interface for discovery
type noopLister struct{}

func (n *noopLister) ExternalAddresses() []string { return []string{} }
func (n *noopLister) AllAddresses() []string      { return []string{} }
