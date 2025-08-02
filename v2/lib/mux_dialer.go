package lib

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/acheong08/syndicate/v2/lib/mux"
	"github.com/syncthing/syncthing/lib/protocol"
)

// MultiplexingDialer provides an enhanced dialer with connection multiplexing for .syncthing domains
type MultiplexingDialer struct {
	tunnelManager *mux.TunnelManager
	baseDialer    net.Dialer
}

// NewMultiplexingDialer creates a new multiplexing-enabled dialer
func NewMultiplexingDialer(cert tls.Certificate, baseDialer interface {
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
}, opts ...MultiplexingDialerOption) *MultiplexingDialer {
	tunnelOpts := []mux.TunnelManagerOption{
		mux.WithMaxPoolSize(5),
		mux.WithMaxIdleTime(5 * time.Minute),
		mux.WithConnectTimeout(30 * time.Second),
	}

	d := &MultiplexingDialer{
		tunnelManager: mux.NewTunnelManager(cert, baseDialer, tunnelOpts...),
		baseDialer:    net.Dialer{Timeout: 10 * time.Second},
	}

	// Apply any custom options
	for _, opt := range opts {
		opt(d)
	}

	return d
}

// MultiplexingDialerOption configures the MultiplexingDialer
type MultiplexingDialerOption func(*MultiplexingDialer)

// WithMuxBaseDialer sets the base dialer for non-.syncthing domains
func WithMuxBaseDialer(dialer net.Dialer) MultiplexingDialerOption {
	return func(d *MultiplexingDialer) {
		d.baseDialer = dialer
	}
}

// WithTunnelOptions sets tunnel manager options
func WithTunnelOptions(opts ...mux.TunnelManagerOption) MultiplexingDialerOption {
	return func(d *MultiplexingDialer) {
		// Note: In a real implementation, we'd need to recreate the tunnel manager
		// with the new options. For now, this is a placeholder.
	}
}

// Dial dials a network connection, using multiplexing for .syncthing domains
func (d *MultiplexingDialer) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	// Check if it's a .syncthing domain
	if !strings.HasSuffix(host, ".syncthing") {
		return d.baseDialer.DialContext(ctx, network, addr)
	}

	// Parse device ID from .syncthing domain
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return nil, ErrInvalidSyncthingDomain
	}

	deviceIDStr := parts[len(parts)-2] // deviceID is second-to-last part
	deviceID, err := protocol.DeviceIDFromString(deviceIDStr)
	if err != nil {
		return nil, err
	}

	// Use tunnel manager for multiplexed connection
	return d.tunnelManager.GetConnection(ctx, deviceID)
}

// Close closes the multiplexing dialer and all its connections
func (d *MultiplexingDialer) Close() error {
	return d.tunnelManager.Close()
}

// ErrInvalidSyncthingDomain is returned when a .syncthing domain is malformed
var ErrInvalidSyncthingDomain = fmt.Errorf("invalid .syncthing domain format")
