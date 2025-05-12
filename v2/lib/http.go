package lib

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/acheong08/syndicate/v2/lib/relay"
	"github.com/rotisserie/eris"
	"github.com/syncthing/syncthing/lib/relay/protocol"
)

func ServeMux(ctx context.Context, relayAddress string, mux *http.ServeMux, cert tls.Certificate) error {
	// if numRelays < 1 {
	// 	numRelays = 1
	// }
	// relays, err := relay.FindOptimal(ctx, "DE", numRelays)
	// if err != nil {
	// 	return err
	// }
	//
	// if len(relays.Relays) < numRelays {
	// 	return fmt.Errorf("not enough relays available")
	// }
	connChan := make(chan net.Conn)
	listener := ConnListener{connChan, make(chan struct{})}
	// for i := range min(len(relays.Relays), numRelays) {
	invite, err := relay.Listen(ctx, relayAddress, cert)
	if err != nil {
		return eris.Wrap(err, "could not listen on relay")
	}
	go func(invites <-chan protocol.SessionInvitation) {
		for {
			select {
			case invite := <-invites:
				conn, _, err := relay.CreateSession(ctx, invite, cert, nil)
				if err != nil {
					log.Printf("ERROR: Relay session creation failed with %s", err)
					continue
				}
				connChan <- conn
			case <-ctx.Done():
				return
			}
		}
	}(invite)
	// }
	return http.Serve(listener, mux)
}

type ConnListener struct {
	connCh <-chan net.Conn
	closed chan struct{}
}

func (l ConnListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connCh:
		return conn, nil
	case <-l.closed:
		return nil, fmt.Errorf("listener closed")
	}
}

func (l ConnListener) Close() error {
	close(l.closed)
	return nil
}

func (l ConnListener) Addr() net.Addr {
	return &net.TCPAddr{} // or a custom Addr
}
