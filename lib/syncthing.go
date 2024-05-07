package lib

import (
	"context"
	"crypto/tls"

	"github.com/syncthing/syncthing/lib/connections/registry"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/events"
	"github.com/syncthing/syncthing/lib/protocol"
)

const SYNCTHING_DISCOVERY_URL = "https://discovery.syncthing.net/v2/?id=LYXKCHX-VI3NYZR-ALCJBHF-WMZYSPK-QG6QJA3-MPFYMSO-U56GTUK-NA2MIAW"

type syncthing struct {
	disco  discover.FinderService
	ctx    context.Context
	cancel context.CancelFunc
}

// NewSyncthing creates a new syncthing instance
// The lister should internally point to a modifiable list.
func NewSyncthing(cert tls.Certificate, lister discover.AddressLister, serve bool) (*syncthing, error) {
	disco, err := discover.NewGlobal(SYNCTHING_DISCOVERY_URL, cert, lister, events.NoopLogger, registry.New())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	if serve {
		go disco.Serve(ctx)
	}
	return &syncthing{
		disco:  disco,
		ctx:    ctx,
		cancel: cancel,
	}, err
}

func (s *syncthing) Lookup(id protocol.DeviceID) ([]string, error) {
	return s.disco.Lookup(s.ctx, id)
}

func (s *syncthing) Close() {
	s.cancel()
}
