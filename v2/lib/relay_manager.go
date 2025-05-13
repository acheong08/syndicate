package lib

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/acheong08/syndicate/v2/lib/discovery"
	"github.com/acheong08/syndicate/v2/lib/relay"
	"github.com/syncthing/syncthing/lib/protocol"
)

const (
	relayTargetCount  = 5
	relayMinThreshold = 3
	relayRetryDelay   = 10 * time.Second
)

type relayListener struct {
	url     string
	stop    context.CancelFunc
	running bool
}

type relayOut struct {
	Conn net.Conn
	Sni  string
}

func StartRelayManager(ctx context.Context, cert tls.Certificate, trustedIds []protocol.DeviceID, relayCountry string) <-chan relayOut {
	var (
		relayMu  sync.Mutex
		relayMap = make(map[string]*relayListener)
	)

	relayChan := make(chan relayOut, 10)

	addressLister := discovery.NewAddressLister()
	go discovery.Broadcast(ctx, cert, &addressLister, discovery.GetDiscoEndpoint(discovery.OptDiscoEndpointAuto))
	startRelayListener := func(ctx context.Context, relayURL string) {
		ctxRelay, stop := context.WithCancel(ctx)
		relayMu.Lock()
		if _, exists := relayMap[relayURL]; exists {
			relayMu.Unlock()
			stop()
			return
		}
		rl := &relayListener{url: relayURL, stop: stop, running: true}
		relayMap[relayURL] = rl
		relayMu.Unlock()

		go func() {
			defer func() {
				relayMu.Lock()
				delete(relayMap, relayURL)
				relayMu.Unlock()
			}()
			invites, err := relay.Listen(ctxRelay, relayURL, cert)
			if err != nil {
				log.Printf("Failed to listen on relay %s: %v", relayURL, err)
				return
			}
			for {
				select {
				case <-ctxRelay.Done():
					return
				case inv, ok := <-invites:
					if !ok {
						log.Printf("Invite channel closed for relay %s", relayURL)
						return
					}
					if len(trustedIds) != 0 && !isTrusted(trustedIds, inv.From) {
						continue
					}
					conn, sni, err := relay.CreateSession(ctxRelay, inv, cert, nil)
					if err != nil {
						log.Printf("Error on invite session for relay %s: %s", relayURL, err)
						continue
					}
					// Avoid sending to relayChan while holding relayMu
					select {
					case relayChan <- relayOut{
						Conn: conn,
						Sni:  sni,
					}:
					case <-ctxRelay.Done():
						conn.Close()
						return
					}
				}
			}
		}()
	}

	go func() {
		for {
			var activeCount int
			relayMu.Lock()
			activeCount = len(relayMap)
			relayMu.Unlock()
			if activeCount < relayMinThreshold {
				log.Printf("Active relays (%d) below threshold (%d), searching for new relays in country: %s...", activeCount, relayMinThreshold, relayCountry)
				relays, err := relay.FindOptimal(ctx, relayCountry, relayTargetCount)
				if err != nil {
					log.Printf("Failed to find relays: %v", err)
					time.Sleep(relayRetryDelay)
					continue
				}
				// Fallback to global if no relays found for the country
				if len(relays.Relays) == 0 && relayCountry != "" {
					log.Printf("No relays found for country '%s', falling back to global relay search.", relayCountry)
					relays, err = relay.FindOptimal(ctx, "", relayTargetCount)
					if err != nil {
						log.Printf("Failed to find global relays: %v", err)
						time.Sleep(relayRetryDelay)
						continue
					}
				}
				addressLister.UpdateAddresses(relays.ToSlice())
				for _, r := range relays.Relays {
					// startRelayListener may lock relayMu, so do not hold it here
					startRelayListener(ctx, r.URL)
				}
			}
			time.Sleep(relayRetryDelay)
		}
	}()
	return relayChan
}

func isTrusted(trustedIds []protocol.DeviceID, from []byte) bool {
	return slices.ContainsFunc(trustedIds, func(id protocol.DeviceID) bool {
		return slices.Equal(from, id[:])
	})
}
