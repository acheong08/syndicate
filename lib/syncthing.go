package lib

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net"
	"net/url"
	"time"

	"gitlab.torproject.org/acheong08/syndicate/lib/relay"
	"gitlab.torproject.org/acheong08/syndicate/lib/utils"

	"github.com/rotisserie/eris"
	"github.com/syncthing/syncthing/lib/connections/registry"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/events"
	syncthingprotocol "github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay/client"
	"github.com/syncthing/syncthing/lib/relay/protocol"
)

const SYNCTHING_ANNOUNCE_URL = "https://discovery-announce-v4.syncthing.net/v2/?nolookup"
const SYNCTHING_LOOKUP_URL = "https://discovery-lookup.syncthing.net/v2/?noannounce"

type Syncthing struct {
	disco discover.FinderService
	ctx   context.Context
}

// NewSyncthing creates a new syncthing instance
// The lister should internally point to a modifiable list.
func NewSyncthing(ctx context.Context, cert tls.Certificate, lister *relay.AddressLister, announce bool) (*Syncthing, error) {
	var list discover.AddressLister
	if lister != nil {
		list = *lister
	} else {
		list = relay.AddressLister{}
	}
	discoveryUrl := SYNCTHING_LOOKUP_URL
	if announce {
		discoveryUrl = SYNCTHING_ANNOUNCE_URL
	}
	disco, err := discover.NewGlobal(discoveryUrl, cert, list, events.NoopLogger, registry.New())
	if err != nil {
		return nil, err
	}
	return &Syncthing{
		disco: disco,
		ctx:   ctx,
	}, err
}

func (s *Syncthing) Serve() {
	go s.disco.Serve(s.ctx)
}

func (s *Syncthing) Lookup(id syncthingprotocol.DeviceID) ([]url.URL, error) {
	addresses, err := s.disco.Lookup(s.ctx, id)
	if err != nil {
		return nil, eris.Wrap(err, "syncthing discovery lookup failed")
	}
	urls := make([]url.URL, len(addresses))
	for i, addr := range addresses {
		url, err := url.Parse(addr)
		if err != nil {
			return nil, eris.Wrapf(err, "%s is not a valid URL", addr)
		}
		urls[i] = *url
	}
	return urls, nil
}

func ConnectToRelay(ctx context.Context, relayAddress *url.URL, cert tls.Certificate, deviceID syncthingprotocol.DeviceID, timeout time.Duration, useTls bool) (net.Conn, error) {
	invite, err := client.GetInvitationFromRelay(ctx, relayAddress, deviceID, []tls.Certificate{cert}, timeout)
	if err != nil {
		return nil, eris.Wrap(err, "Failed to get relay invitation")
	}

	conn, err := client.JoinSession(ctx, invite)
	if err != nil {
		return nil, eris.Wrap(err, "Failed to join relay session")
	}
	if !useTls {
		return conn, nil
	}
	return utils.UpgradeClientConn(conn, cert)
}

func ListenSingleRelay(cert tls.Certificate, relayAddress string, clientID syncthingprotocol.DeviceID, clientCert *x509.Certificate) (net.Conn, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connChan := make(chan net.Conn)
	err := ListenRelay(ctx, cert, relayAddress, &clientID, clientCert, connChan)
	if err != nil {
		return nil, eris.Wrap(err, "Relay listener failed")
	}
	return <-connChan, nil
}

func ListenRelay(ctx context.Context, serverCert tls.Certificate, relayAddress string, clientID *syncthingprotocol.DeviceID, clientCert *x509.Certificate, connChan chan net.Conn) error {
	relayURL, _ := url.Parse(relayAddress)
	// Make a connection to the relay
	relay, err := client.NewClient(relayURL, []tls.Certificate{serverCert}, time.Second*10)
	if err != nil {
		return eris.Wrap(err, "Could not create relay client. This should never happen")
	}
	go relay.Serve(ctx)

	inviteRecv := make(chan protocol.SessionInvitation, 100)
	go func() {
		for invite := range relay.Invitations() {
			log.Println("Received invite from", invite)
			fromDevice, _ := syncthingprotocol.DeviceIDFromBytes(invite.From)
			if clientID != nil && !fromDevice.Equals(*clientID) {
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

	go func() {
		for {
			select {
			case invite := <-inviteRecv:
				conn, err := client.JoinSession(ctx, invite)
				if err != nil {
					log.Println("Could not join session with invite", invite)
					continue
				}
				log.Println("Connected to", conn.RemoteAddr())
				if clientCert == nil {
					log.Println("Using plain connection")
					connChan <- conn
					continue
				}
				tlsConn, err := utils.UpgradeServerConn(conn, serverCert, clientCert)
				if err != nil {
					log.Println("Failed to upgrade connection to TLS")
					continue
				}
				connChan <- tlsConn
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func FindOptimalRelay(country string) (string, error) {
	relays, err := relay.FetchRelays()
	if err != nil {
		return "", err
	}
	// relays.Filter(func(r relay.Relay) bool {
	// 	if country == "" {
	// 		return true
	// 	}
	// 	return r.Location.Country == country
	// })
	// log.Printf("Found %d relays", len(relays.Relays))
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
			log.Println("Successfully connected to", relayURL.String())
			return relay.URL, nil
		}
	}
	return "", eris.New("No viable relays found")
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
