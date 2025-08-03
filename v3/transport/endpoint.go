package transport

import (
	"fmt"
	"net"
	"strings"

	"github.com/syncthing/syncthing/lib/protocol"
)

// RelayEndpoint represents a Syncthing relay endpoint
type RelayEndpoint struct {
	deviceID protocol.DeviceID
	sni      string
	relays   []string
}

// NewRelayEndpoint creates a new relay endpoint from a .syncthing domain
func NewRelayEndpoint(address string) (*RelayEndpoint, error) {
	// Parse address format: [sni.]deviceid.syncthing[:port]
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		// If no port, use the address as host
		host = address
	}

	if !strings.HasSuffix(host, ".syncthing") {
		return nil, fmt.Errorf("invalid relay address: must end with .syncthing")
	}

	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid relay address format: %s", address)
	}

	// Extract device ID (second-to-last part before .syncthing)
	deviceIDStr := parts[len(parts)-2]
	deviceID, err := protocol.DeviceIDFromString(deviceIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid device ID in address %s: %v", address, err)
	}

	// Extract SNI if present (everything before deviceid.syncthing)
	var sni string
	if len(parts) > 2 {
		sni = strings.Join(parts[0:len(parts)-2], ".")
	}
	// If no SNI is specified, leave it empty (don't default to device ID)

	return &RelayEndpoint{
		deviceID: deviceID,
		sni:      sni,
	}, nil
}

// NewRelayEndpointWithRelays creates a relay endpoint with specific relay URLs
func NewRelayEndpointWithRelays(deviceID protocol.DeviceID, sni string, relays []string) *RelayEndpoint {
	return &RelayEndpoint{
		deviceID: deviceID,
		sni:      sni,
		relays:   relays,
	}
}

// Address returns the .syncthing domain representation
func (e *RelayEndpoint) Address() string {
	if e.sni != "" {
		return fmt.Sprintf("%s.%s.syncthing", e.sni, e.deviceID.String())
	}
	return fmt.Sprintf("%s.syncthing", e.deviceID.String())
}

// Network returns "relay"
func (e *RelayEndpoint) Network() string {
	return "relay"
}

// Metadata returns relay-specific metadata
func (e *RelayEndpoint) Metadata() map[string]interface{} {
	meta := map[string]interface{}{
		"device_id": e.deviceID.String(),
	}
	if e.sni != "" {
		meta["sni"] = e.sni
	}
	if len(e.relays) > 0 {
		meta["relays"] = e.relays
	}
	return meta
}

// DeviceID returns the target device ID
func (e *RelayEndpoint) DeviceID() protocol.DeviceID {
	return e.deviceID
}

// SNI returns the Server Name Indication
func (e *RelayEndpoint) SNI() string {
	return e.sni
}

// Relays returns the list of relay URLs, if set
func (e *RelayEndpoint) Relays() []string {
	return e.relays
}

// SetRelays updates the relay URL list
func (e *RelayEndpoint) SetRelays(relays []string) {
	e.relays = relays
}
