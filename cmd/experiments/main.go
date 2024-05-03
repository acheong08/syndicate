package main

import (
	"context"
	"fmt"
	"net/url"
	"syndicate/lib/relay"
	"time"

	"github.com/syncthing/syncthing/lib/connections/registry"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/events"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/tlsutil"
)

const SYNCTHING_DISCOVERY_URL = "https://discovery.syncthing.net/v2/?id=LYXKCHX-VI3NYZR-ALCJBHF-WMZYSPK-QG6QJA3-MPFYMSO-U56GTUK-NA2MIAW"

func main() {
	cert, _ := tlsutil.NewCertificateInMemory("syncthing", 1)
	myId := protocol.NewDeviceID(cert.Certificate[0])
	fmt.Println(myId)
	// Show relays
	relays, err := relay.FetchRelays()
	if err != nil {
		fmt.Println(err)
		return
	}
	relays = relays.Filter(func(r relay.Relay) bool {
		return r.Location.Country == "GB"
	})
	// for _, r := range relays.Relays {
	// 	fmt.Println(r)
	// }
	chosenOne := relays.Relays[0] // Just take the first one as a test
	lister := relay.AddressLister{RelayAddress: chosenOne.URL}
	disco, err := discover.NewGlobal(SYNCTHING_DISCOVERY_URL, cert, lister, events.NoopLogger, registry.New())
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	go disco.Serve(ctx)
	defer cancel()

	t0 := time.Now()
	for err := disco.Error(); err != nil; err = disco.Error() {
		if time.Since(t0) > 10*time.Second {
			fmt.Println("Timeout")
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	addresses, err := disco.Lookup(ctx, myId)
	if err != nil {
		fmt.Println(err)
		return
	}
	addrURL, _ := url.Parse(addresses[0])
	expectedURL, _ := url.Parse(chosenOne.URL)
	if addrURL.Host != expectedURL.Host {
		fmt.Println("Unexpected address: ", addrURL.Host)
		return
	}
	if addrURL.Scheme != "relay" {
		fmt.Println("Unexpected scheme: ", addrURL.Scheme)
		return
	}
	// Get ID of relay
	if addrURL.Query().Get("id") != expectedURL.Query().Get("id") {
		fmt.Println("Unexpected ID: ", addrURL.Query().Get("id"))
		return
	}
}
