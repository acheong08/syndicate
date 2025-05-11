package discovery

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/rotisserie/eris"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/events"
	"github.com/syncthing/syncthing/lib/protocol"
)

func isIPv4Available() bool {
	conn, err := net.DialTimeout("tcp4", "example.com:80", 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func isIPv6Available() bool {
	conn, err := net.DialTimeout("tcp6", "example.com:80", 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

type DiscoveryEndpoints struct {
	Lookup   string
	Announce string
}

var discoveryEndpointsV6 = DiscoveryEndpoints{config.DefaultDiscoveryServersV6[0], config.DefaultDiscoveryServersV6[1]}
var discoveryEndpointsV4 = DiscoveryEndpoints{config.DefaultDiscoveryServersV4[0], config.DefaultDiscoveryServersV4[1]}

func getDiscoveryEndpointsAuto() DiscoveryEndpoints {
	if isIPv6Available() {
		return discoveryEndpointsV6
	}
	if isIPv4Available() {
		return discoveryEndpointsV4
	}
	panic("both ipv4 and ipv6 are unavailable")
}

type DiscoveryEndpointOption uint

const (
	OptDiscoEndpointV4 DiscoveryEndpointOption = iota
	OptDiscoEndpointV6
	OptDiscoEndpointAuto
)

type noopLister struct{}

func (n noopLister) ExternalAddresses() []string {
	return []string{}
}

func (n noopLister) AllAddresses() []string {
	return []string{}
}

func LookupDevice(ctx context.Context, cert tls.Certificate, deviceId protocol.DeviceID, endpointOption DiscoveryEndpointOption) ([]string, error) {
	var discoEndpoints DiscoveryEndpoints
	switch endpointOption {
	case OptDiscoEndpointV4:
		discoEndpoints = discoveryEndpointsV4
	case OptDiscoEndpointV6:
		discoEndpoints = discoveryEndpointsV6
	case OptDiscoEndpointAuto:
		discoEndpoints = getDiscoveryEndpointsAuto()
	}
	disco, err := discover.NewGlobal(discoEndpoints.Lookup, cert, noopLister{}, events.NoopLogger, nil)
	if err != nil {
		return nil, eris.Wrap(err, "failed to create discovery service")
	}
	addresses, err := disco.Lookup(ctx, deviceId)
	if err != nil {
		return nil, eris.Wrap(err, "address lookup failed")
	}
	return addresses, nil
}
