package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/acheong08/syndicate/v2/lib/mux"
	"github.com/syncthing/syncthing/lib/protocol"
)

func main() {
	cert, _ := crypto.NewCertificate("syncthing", 1)
	log.Printf("Server ID: %s", protocol.NewDeviceID(cert.Certificate[0]))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create HTTP handlers for different subdomains
	mux1 := http.NewServeMux()
	mux1.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello from subdomain 1 (multiplexed)"))
	})

	mux2 := http.NewServeMux()
	mux2.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello from subdomain 2 (multiplexed)"))
	})

	// Start relay manager
	relayChan := lib.StartRelayManager(ctx, cert, []protocol.DeviceID{}, "DE")

	// Handle incoming relay connections with multiplexing
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case relayOut := <-relayChan:
				go handleMultiplexedRelayConnection(ctx, relayOut.Conn, relayOut.Sni, mux1, mux2)
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait()
}

// handleMultiplexedRelayConnection handles a single relay connection with multiplexing
func handleMultiplexedRelayConnection(ctx context.Context, conn net.Conn, sni string, mux1, mux2 *http.ServeMux) {
	defer conn.Close()

	// Create server session for multiplexing
	session := mux.NewServerSession(ctx, conn)
	defer session.Close()

	log.Printf("New multiplexed HTTP connection from %s (SNI: %s)", conn.RemoteAddr(), sni)

	// Choose the appropriate HTTP handler based on SNI
	var handler http.Handler
	switch sni {
	case "1":
		handler = mux1
	case "2":
		handler = mux2
	default:
		log.Printf("Unknown SNI: %s, using default handler", sni)
		handler = mux1
	}

	// Accept streams and handle each as an HTTP connection
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			log.Printf("Failed to accept stream: %v", err)
			return
		}

		// Handle each stream as a separate HTTP connection
		go func(s net.Conn) {
			defer s.Close()
			if err := http.Serve(&singleConnListener{conn: s}, handler); err != nil {
				log.Printf("HTTP serve error: %v", err)
			}
		}(stream)
	}
}

// singleConnListener is a net.Listener that returns a single connection once
type singleConnListener struct {
	conn net.Conn
	used bool
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	if l.used {
		// Block forever after first use
		select {}
	}
	l.used = true
	return l.conn, nil
}

func (l *singleConnListener) Close() error {
	return l.conn.Close()
}

func (l *singleConnListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}
