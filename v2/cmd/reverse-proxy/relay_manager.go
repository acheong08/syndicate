package main

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

func StartRelayManager(ctx context.Context, cert tls.Certificate, trustedIds []protocol.DeviceID, connChan chan net.Conn, relayCountry string) {
	var (
		relayMu  sync.Mutex
		relayMap = make(map[string]*relayListener)
	)

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
					conn, _, err := relay.CreateSession(ctxRelay, inv, cert, nil)
					if err != nil {
						log.Printf("Error on invite session for relay %s: %s", relayURL, err)
						continue
					}
					connChan <- conn
				}
			}
		}()
	}

	go func() {
		for {
			relayMu.Lock()
			activeCount := len(relayMap)
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
				addresses := relays.ToSlice()
				go discovery.Broadcast(ctx, cert, discovery.AddressLister{Addresses: addresses}, discovery.GetDiscoEndpoint(discovery.OptDiscoEndpointAuto))
				for _, r := range relays.Relays {
					startRelayListener(ctx, r.URL)
				}
			}
			time.Sleep(relayRetryDelay)
		}
	}()
}

func isTrusted(trustedIds []protocol.DeviceID, from []byte) bool {
	return slices.ContainsFunc(trustedIds, func(id protocol.DeviceID) bool {
		return slices.Equal(from, id[:])
	})
}
