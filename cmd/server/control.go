package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net/url"
	"syndicate/lib"
	"syndicate/lib/commands"
	"syndicate/lib/relay"
	"syndicate/lib/utils"
	"time"

	syncthingprotocol "github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay/client"
	"github.com/syncthing/syncthing/lib/relay/protocol"
)

func controlClient(clientEntry lib.ClientEntry, command commands.Command, countryCode string) error {
	commandBytes := []byte{byte(command)}
	ips, ports, err := utils.EncodeIPv6(commandBytes, clientEntry.ClientID)
	if err != nil {
		panic(err)
	}
	theRelay, err := findOptimalRelay(countryCode)
	if err != nil {
		return err
	}
	lister := relay.AddressLister{
		IPs:          ips,
		Ports:        ports,
		RelayAddress: theRelay,
	}
	cert, err := tls.X509KeyPair(clientEntry.ServerCert[0], clientEntry.ServerCert[1])
	if err != nil {
		panic(err)
	}
	syncthing, err := lib.NewSyncthing(cert, &lister)
	if err != nil {
		panic(err)
	}
	syncthing.Serve()
	defer syncthing.Close()
	relayURL, _ := url.Parse(theRelay)
	// Make a connection to the relay
	relay, err := client.NewClient(relayURL, []tls.Certificate{cert}, time.Second*10)
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go relay.Serve(ctx)

	inviteRecv := make(chan protocol.SessionInvitation)
	go func() {
		for invite := range relay.Invitations() {
			log.Println("Received invite from", invite)
			fromDevice, _ := syncthingprotocol.DeviceIDFromBytes(invite.From)
			if !fromDevice.Equals(clientEntry.ClientID) {
				log.Println("Discarding invite from unknown client")
				continue
			}
			select {
			case inviteRecv <- invite:
				log.Println("Sent invite to recv")
			default:
				log.Println("Discarded invite")
			}
		}
	}()

	conn, err := client.JoinSession(ctx, <-inviteRecv)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	log.Printf("Connected to %s", conn.RemoteAddr())
	// Upgrade to TLS
	clientCert, err := x509.ParseCertificate(clientEntry.ClientCert)
	if err != nil {
		return err
	}
	_, err = utils.UpgradeServerConn(conn, cert, clientCert, time.Second*5)
	return err
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

	return relays.Relays[0].URL, nil
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
