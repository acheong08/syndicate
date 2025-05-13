package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"strings"
	"time"

	"github.com/acheong08/syndicate/v2/internal"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/acheong08/syndicate/v2/lib/discovery"
	"github.com/acheong08/syndicate/v2/lib/relay"
	"github.com/rotisserie/eris"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/things-go/go-socks5"
)

type HybridDialer struct {
	ClientCert        tls.Certificate
	Timeout           time.Duration
	BaseDialer        net.Dialer // For normal traffic
	DiscoveryEndpoint discovery.DiscoveryEndpoints
	relayCache        map[protocol.DeviceID]relayCacheEntry
	relayCacheTTL     time.Duration
}

// HybridDialerOption is a functional option for HybridDialer
type HybridDialerOption func(*HybridDialer)

// WithTimeout sets the timeout for the HybridDialer
func WithTimeout(timeout time.Duration) HybridDialerOption {
	return func(h *HybridDialer) {
		h.Timeout = timeout
	}
}

// WithBaseDialer sets the base dialer for the HybridDialer
func WithBaseDialer(dialer net.Dialer) HybridDialerOption {
	return func(h *HybridDialer) {
		h.BaseDialer = dialer
	}
}

// WithDiscoveryEndpoint sets the discovery endpoint for the HybridDialer
func WithDiscoveryEndpoint(endpoint discovery.DiscoveryEndpoints) HybridDialerOption {
	return func(h *HybridDialer) {
		h.DiscoveryEndpoint = endpoint
	}
}

// WithRelayCacheTTL sets the relay cache TTL for the HybridDialer
func WithRelayCacheTTL(ttl time.Duration) HybridDialerOption {
	return func(h *HybridDialer) {
		h.relayCacheTTL = ttl
	}
}

// NewHybridDialer constructs a HybridDialer with safe defaults and applies any options.
func NewHybridDialer(cert tls.Certificate, opts ...HybridDialerOption) *HybridDialer {
	h := &HybridDialer{
		ClientCert:        cert,
		Timeout:           10 * time.Second,
		BaseDialer:        net.Dialer{},
		DiscoveryEndpoint: discovery.GetDiscoEndpoint(discovery.OptDiscoEndpointAuto),
		relayCache:        make(map[protocol.DeviceID]relayCacheEntry),
		relayCacheTTL:     5 * time.Minute,
	}
	for _, opt := range opts {
		opt(h)
	}
	// Ensure relayCache is always non-nil
	if h.relayCache == nil {
		h.relayCache = make(map[protocol.DeviceID]relayCacheEntry)
	}
	// Ensure DiscoveryEndpoint is set
	if h.DiscoveryEndpoint.Lookup == "" && h.DiscoveryEndpoint.Announce == "" {
		h.DiscoveryEndpoint = discovery.GetDiscoEndpoint(discovery.OptDiscoEndpointAuto)
	}
	// Ensure Timeout is set
	if h.Timeout == 0 {
		h.Timeout = 10 * time.Second
	}
	// Ensure relayCacheTTL is set
	if h.relayCacheTTL == 0 {
		h.relayCacheTTL = 5 * time.Minute
	}
	return h
}

type relayCacheEntry struct {
	relays    []string
	timestamp time.Time
}

func (d *HybridDialer) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	// Check if it's a .syncthing domain
	if !strings.HasSuffix(host, ".syncthing") {
		return d.BaseDialer.DialContext(ctx, network, addr)
	}
	// log.Printf("Dailing relay %s", host)
	// relayCache is guaranteed non-nil by constructor
	conn, err := d.dialRelay(ctx, network, host)
	if err != nil {
		log.Println("Dial error: ", err)
		return nil, err
	}
	return conn, nil
}

func (d *HybridDialer) dialRelay(ctx context.Context, _, host string) (net.Conn, error) {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid .syncthing domain format")
	}
	deviceIDStr := parts[len(parts)-2] // deviceID is second-to-last part

	deviceID, err := protocol.DeviceIDFromString(deviceIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid device ID: %v", err)
	}

	var relays []string
	now := time.Now()
	entry, ok := d.relayCache[deviceID]
	if ok && now.Sub(entry.timestamp) < d.relayCacheTTL {
		relays = entry.relays
	} else {
		relays, err = discovery.LookupDevice(ctx, deviceID, d.DiscoveryEndpoint)
		if err != nil {
			return nil, eris.Wrap(err, "failed to look up device")
		}
		d.relayCache[deviceID] = relayCacheEntry{
			relays:    relays,
			timestamp: now,
		}
	}

	// Get invite and establish relay connection
	invite, err := relay.GetInvite(ctx, relays[rand.IntN(len(relays))], deviceID, d.ClientCert)
	if err != nil {
		return nil, fmt.Errorf("failed to get invite: %v", err)
	}

	sni := ""
	if len(parts) > 2 {
		sni = strings.Join(parts[0:len(parts)-2], ".")
	}
	conn, _, err := relay.CreateSession(ctx, invite, d.ClientCert, &sni)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %v", err)
	}

	return conn, nil
}

// DNSResolver uses the system DNS to resolve host names
type DNSResolver struct{}

// Resolve implement interface NameResolver
func (d DNSResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	if strings.HasSuffix(name, ".syncthing") {
		return ctx, net.IP{}, nil
	}
	addr, err := net.ResolveIPAddr("ip", name)
	if err != nil {
		return ctx, nil, err
	}
	return ctx, addr.IP, err
}

func main() {
	keysPath := flag.String("keys", "", "Path to gob encoded KeyPair")
	flag.Parse()
	var cert tls.Certificate
	var err error
	if keysPath == nil || *keysPath == "" {
		cert, err = crypto.NewCertificate("syncthing-client", 1)
	} else {
		cert, err = internal.ReadKeyPair(*keysPath)
	}
	if err != nil {
		panic(err)
	}
	log.Printf("Starting socks with ID %s", protocol.NewDeviceID(cert.Certificate[0]))

	// Create hybrid dialer with both capabilities
	dialer := NewHybridDialer(cert)

	server := socks5.NewServer(socks5.WithDial(dialer.Dial), socks5.WithResolver(DNSResolver{}))

	// Start server
	fmt.Println("Starting SOCKS5 server on :1080")
	if err := server.ListenAndServe("tcp", "127.0.0.1:1080"); err != nil {
		panic(err)
	}
}
