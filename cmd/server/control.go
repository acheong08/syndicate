package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"time"

	"gitlab.torproject.org/acheong08/syndicate/lib"
	"gitlab.torproject.org/acheong08/syndicate/lib/relay"
)

func startBroadcast(ctx context.Context, cert tls.Certificate, relayAddress string) error {
	lister := relay.AddressLister{
		RelayAddress: relayAddress,
	}
	syncthing, err := lib.NewSyncthing(ctx, cert, &lister)
	if err != nil {
		return err
	}
	syncthing.Serve()
	return nil
}

func findOptimalRelay(country string) (string, error) {
	relays, err := relay.FetchRelays()
	if err != nil {
		return "", err
	}
	relays.Filter(func(r relay.Relay) bool {
		return r.Location.Country == country
	})
	relays.Sort(func(a, b relay.Relay) bool {
		// Use a heuristic to determine the best relay
		var aScore, bScore int
		if a.Stats.NumActiveSessions > b.Stats.NumActiveSessions {
			aScore += 1
		} else {
			// We don't add if they are equal
			bScore += btoi(!(a.Stats.NumActiveSessions == b.Stats.NumActiveSessions))
		}
		if a.Stats.UptimeSeconds > b.Stats.UptimeSeconds {
			aScore++
		} else {
			bScore += btoi(!(a.Stats.UptimeSeconds == b.Stats.UptimeSeconds))
		}
		aRate := minButNotZero(a.Stats.Options.GlobalRate, a.Stats.Options.PerSessionRate)
		bRate := minButNotZero(b.Stats.Options.GlobalRate, b.Stats.Options.PerSessionRate)
		if aRate > bRate {
			aScore++
		} else {
			bScore += btoi(!(aRate == bRate))
		}

		return aScore > bScore
	})

	for _, relay := range relays.Relays {
		// Test connection
		relayURL, _ := url.Parse(relay.URL)
		timeout := time.Second * 5
		conn, err := net.DialTimeout("tcp", relayURL.Host, timeout)
		if err != nil {
			log.Printf("Failed to connect to %s: %s", relay.URL, err)
			continue
		}
		if conn != nil {
			defer conn.Close()
			fmt.Println("Successfully connected to", relayURL.String())
			return relay.URL, nil
		}
	}
	return "", errors.New("No viable relays found")
}

func minButNotZero(a, b int) int {
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
