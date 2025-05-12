package main

import (
	"context"
	"log"
	"net"
	"net/http"

	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/acheong08/syndicate/v2/lib/discovery"
	"github.com/acheong08/syndicate/v2/lib/relay"
	"github.com/syncthing/syncthing/lib/protocol"
	relayprotocol "github.com/syncthing/syncthing/lib/relay/protocol"
)

func main() {
	cert, _ := crypto.NewCertificate("syncthing", 1)
	log.Printf("Server ID: %s", protocol.NewDeviceID(cert.Certificate[0]))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relays, err := relay.FindOptimal(ctx, "DE", 5)
	if err != nil {
		panic(err)
	}

	go discovery.Broadcast(ctx, cert, discovery.AddressLister{Addresses: relays.ToSlice()}, discovery.GetDiscoEndpoint(discovery.OptDiscoEndpointAuto))

	mux := http.NewServeMux()
	mux.HandleFunc("/eggs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "magic" {
			w.WriteHeader(400)
		}
		w.Write([]byte("Hello world"))
	})
	connChan := make(chan net.Conn, 5)
	for _, r := range relays.Relays {
		invites, err := relay.Listen(ctx, r.URL, cert)
		if err != nil {
			panic(err)
		}
		go func(invites <-chan relayprotocol.SessionInvitation) {
			for {
				select {
				case <-ctx.Done():
					return
				case inv := <-invites:
					conn, sni, err := relay.CreateSession(ctx, inv, cert, nil)
					if err != nil {
						log.Printf("Error on invite session: %s", err)
						conn.Close()
						continue
					}
					log.Printf("Received connection from SNI %s", sni)
					connChan <- conn
				}
			}
		}(invites)
	}
	log.Fatal(lib.ServeMux(ctx, mux, connChan))
}
