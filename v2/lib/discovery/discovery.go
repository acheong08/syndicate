package discovery

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/rotisserie/eris"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/connections/registry"
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

var (
	discoveryEndpointsV6 = DiscoveryEndpoints{config.DefaultDiscoveryServersV6[0], config.DefaultDiscoveryServersV6[1]}
	discoveryEndpointsV4 = DiscoveryEndpoints{config.DefaultDiscoveryServersV4[0], config.DefaultDiscoveryServersV4[1]}
)

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

func GetDiscoEndpoint(endpointOption DiscoveryEndpointOption) DiscoveryEndpoints {
	switch endpointOption {
	case OptDiscoEndpointV4:
		return discoveryEndpointsV4
	case OptDiscoEndpointV6:
		return discoveryEndpointsV6
	case OptDiscoEndpointAuto:
		return getDiscoveryEndpointsAuto()
	default:
		panic("unreachable")
	}
}

func LookupDevice(ctx context.Context, deviceId protocol.DeviceID, discoEndpoint DiscoveryEndpoints) ([]string, error) {
	// Generate certificate since it's not necessary for lookup
	cert, err := crypto.NewCertificate("syncthing", 1) // It doesn't need to last since it's single use

	disco, err := discover.NewGlobal(discoEndpoint.Lookup, cert, noopLister{}, events.NoopLogger, nil)
	if err != nil {
		return nil, eris.Wrap(err, "failed to create discovery service")
	}
	addresses, err := disco.Lookup(ctx, deviceId)
	if err != nil {
		return nil, eris.Wrap(err, "address lookup failed")
	}
	return addresses, nil
}

func Broadcast(ctx context.Context, cert tls.Certificate, lister *addressLister, discoEndpoint DiscoveryEndpoints) error {
	if lister == nil {
		panic("address lister cannot be nil")
	}
	logger := events.NewLogger()
	lister.OnUpdate = func(s []string) {
		logger.Log(events.ListenAddressesChanged, nil)
	}
	go logger.Serve(ctx)
	disco, err := discover.NewGlobal(discoEndpoint.Announce, cert, lister, logger, registry.New())
	if err != nil {
		return eris.Wrap(err, "failed to create discovery service")
	}
	return disco.Serve(ctx)
}

type addressLister struct {
	addresses []string
	OnUpdate  func([]string)
}

func NewAddressLister() addressLister {
	return addressLister{}
}

func (a *addressLister) UpdateAddresses(addresses []string) {
	a.addresses = addresses
	if a.OnUpdate != nil {
		a.OnUpdate(a.addresses)
	}
}

func (a addressLister) ExternalAddresses() []string {
	return a.addresses
}

func (a addressLister) AllAddresses() []string {
	return a.addresses
}
