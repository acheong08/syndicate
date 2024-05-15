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

	"github.com/syncthing/syncthing/lib/connections/registry"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/events"
	syncthingprotocol "github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay/client"
	"github.com/syncthing/syncthing/lib/relay/protocol"
)

const SYNCTHING_DISCOVERY_URL = "https://discovery.syncthing.net/v2/?id=LYXKCHX-VI3NYZR-ALCJBHF-WMZYSPK-QG6QJA3-MPFYMSO-U56GTUK-NA2MIAW"

type Syncthing struct {
	disco discover.FinderService
	ctx   context.Context
}

// NewSyncthing creates a new syncthing instance
// The lister should internally point to a modifiable list.
func NewSyncthing(ctx context.Context, cert tls.Certificate, lister *relay.AddressLister) (*Syncthing, error) {
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

func ListenSingleRelay(cert tls.Certificate, relayAddress string, clientID syncthingprotocol.DeviceID, clientCert *x509.Certificate) (net.Conn, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connChan := make(chan net.Conn)
	err := ListenRelay(ctx, cert, relayAddress, clientID, clientCert, connChan)
	if err != nil {
		return nil, err
	}
	return <-connChan, nil
}

func ListenRelay(ctx context.Context, cert tls.Certificate, relayAddress string, clientID syncthingprotocol.DeviceID, clientCert *x509.Certificate, connChan chan net.Conn) error {
	relayURL, _ := url.Parse(relayAddress)
	// Make a connection to the relay
	relay, err := client.NewClient(relayURL, []tls.Certificate{cert}, time.Second*10)
	if err != nil {
		return err
	}
	go relay.Serve(ctx)

	inviteRecv := make(chan protocol.SessionInvitation)
	go func() {
		for invite := range relay.Invitations() {
			log.Println("Received invite from", invite)
			fromDevice, _ := syncthingprotocol.DeviceIDFromBytes(invite.From)
			if !fromDevice.Equals(clientID) {
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

	// conn, err := client.JoinSession(ctx, <-inviteRecv)
	// if err != nil {
	// 	panic(err)
	// }
	// defer conn.Close()
	// log.Printf("Connected to %s", conn.RemoteAddr())
	// return utils.UpgradeServerConn(conn, cert, clientCert, time.Second*5)
	go func() {
		for {
			select {
			case invite := <-inviteRecv:
				conn, err := client.JoinSession(ctx, invite)
				if err != nil {
					continue
				}
				log.Println("Connected to", conn.RemoteAddr())
				conn, err = utils.UpgradeServerConn(conn, cert, clientCert, time.Second*5)
				if err != nil {
					continue
				}
				connChan <- conn
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}
