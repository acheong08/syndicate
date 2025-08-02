package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net"
	"os"

	"github.com/acheong08/syndicate/v2/internal"
	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/acheong08/syndicate/v2/lib/mux"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/things-go/go-socks5"
)

func main() {
	keysPath := flag.String("keys", "", "Path to gob encoded KeyPair")
	trustedIdsPath := flag.String("trusted", "", "Path to newline separated device IDs")
	country := flag.String("country", "", "Country code for relay selection (auto-detect if empty)")
	flag.Parse()

	var cert tls.Certificate
	var err error
	if keysPath == nil || *keysPath == "" {
		cert, err = crypto.NewCertificate("socks-server", 1)
	} else {
		cert, err = internal.ReadKeyPair(*keysPath)
	}
	if err != nil {
		panic(err)
	}

	var trustedIds []protocol.DeviceID
	if trustedIdsPath != nil && *trustedIdsPath != "" {
		trustedIds, err = loadTrustedDeviceIDs(*trustedIdsPath)
		if err != nil {
			panic(err)
		}
	}

	log.Printf("Starting SOCKS5 server at %s.syncthing/", protocol.NewDeviceID(cert.Certificate[0]))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create SOCKS5 server
	server := socks5.NewServer()

	var relayCountry string
	if *country != "" {
		relayCountry = *country
	} else {
		relayCountry = "DE"
	}

	// Start relay manager to accept incoming connections
	relayChan := lib.StartRelayManager(ctx, cert, trustedIds, relayCountry)

	// Handle incoming relay connections with multiplexing
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case relayOut := <-relayChan:
				go handleRelayConnection(ctx, relayOut.Conn, server)
			}
		}
	}()

	select {}
}

// handleRelayConnection handles a single relay connection with multiplexing
func handleRelayConnection(ctx context.Context, conn net.Conn, server *socks5.Server) {
	defer conn.Close()

	// Create server session for multiplexing
	session := mux.NewServerSession(ctx, conn)
	defer session.Close()

	log.Printf("New multiplexed connection from %s", conn.RemoteAddr())

	// Accept streams and handle each as a SOCKS5 connection
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			log.Printf("Failed to accept stream: %v", err)
			return
		}

		// Handle each stream as a separate SOCKS5 connection
		go func(s net.Conn) {
			defer s.Close()
			if err := server.ServeConn(s); err != nil {
				log.Printf("SOCKS5 serve error: %v", err)
			}
		}(stream)
	}
}

func loadTrustedDeviceIDs(path string) ([]protocol.DeviceID, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	trustedIds := []protocol.DeviceID{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 64 {
			line = line[:64]
		}
		id, err := protocol.DeviceIDFromString(line)
		if err != nil {
			return nil, err
		}
		trustedIds = append(trustedIds, id)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return trustedIds, nil
}
