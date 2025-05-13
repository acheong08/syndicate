package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/syncthing/syncthing/lib/protocol"
)

func main() {
	cert, _ := crypto.NewCertificate("syncthing", 1)
	log.Printf("Server ID: %s", protocol.NewDeviceID(cert.Certificate[0]))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux1 := http.NewServeMux()
	mux1.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello from subdomain 1"))
	})
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello from subdomain 2"))
	})
	relayChan := lib.StartRelayManager(ctx, cert, []protocol.DeviceID{}, "DE")
	mux1Chan := make(chan net.Conn, 5)
	mux2Chan := make(chan net.Conn, 5)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case relayOut := <-relayChan:
				switch relayOut.Sni {
				case "1":
					mux1Chan <- relayOut.Conn
				case "2":
					mux2Chan <- relayOut.Conn
				default:
					relayOut.Conn.Close()
				}
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		log.Fatal(lib.ServeMux(ctx, mux1, mux1Chan))
	}()
	go func() {
		defer wg.Done()
		log.Fatal(lib.ServeMux(ctx, mux2, mux2Chan))
	}()
	wg.Wait()
}
