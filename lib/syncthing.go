package lib

import (
	"context"
	"crypto/tls"
	"net/url"
	"gitlab.torproject.org/acheong08/syndicate/lib/relay"

	"github.com/syncthing/syncthing/lib/connections/registry"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/events"
	"github.com/syncthing/syncthing/lib/protocol"
)

const SYNCTHING_DISCOVERY_URL = "https://discovery.syncthing.net/v2/?id=LYXKCHX-VI3NYZR-ALCJBHF-WMZYSPK-QG6QJA3-MPFYMSO-U56GTUK-NA2MIAW"

type Syncthing struct {
	disco  discover.FinderService
	ctx    context.Context
	cancel context.CancelFunc
}

// NewSyncthing creates a new syncthing instance
// The lister should internally point to a modifiable list.
func NewSyncthing(cert tls.Certificate, lister *relay.AddressLister) (*Syncthing, error) {
	var list discover.AddressLister
	if lister != nil {
		list = *lister
	} else {
		list = relay.AddressLister{}
	}
	disco, err := discover.NewGlobal(SYNCTHING_DISCOVERY_URL, cert, list, events.NoopLogger, registry.New())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Syncthing{
		disco:  disco,
		ctx:    ctx,
		cancel: cancel,
	}, err
}

func (s *Syncthing) Serve() {
	go s.disco.Serve(s.ctx)
}

func (s *Syncthing) Lookup(id protocol.DeviceID) ([]url.URL, error) {
	addresses, err := s.disco.Lookup(s.ctx, id)
	if err != nil {
		return nil, err
	}
	urls := make([]url.URL, len(addresses))
	for i, addr := range addresses {
		url, err := url.Parse(addr)
		if err != nil {
			return nil, err
		}
		urls[i] = *url
	}
	return urls, nil
}

func (s *Syncthing) Close() {
	s.cancel()
}
