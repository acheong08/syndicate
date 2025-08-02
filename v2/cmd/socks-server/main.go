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

	// Channel to receive connections from syncthing relay
	connChan := make(chan net.Conn, 5)

	var relayCountry string
	if *country != "" {
		relayCountry = *country
	} else {
		relayCountry = "DE"
	}

	// Start relay manager
	relayChan := lib.StartRelayManager(ctx, cert, trustedIds, relayCountry)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case relayOut := <-relayChan:
				connChan <- relayOut.Conn
			}
		}
	}()

	// Handle incoming connections
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case conn := <-connChan:
				go func(c net.Conn) {
					defer c.Close()
					if err := server.ServeConn(c); err != nil {
						log.Printf("SOCKS5 serve error: %v", err)
					}
				}(conn)
			}
		}
	}()

	select {}
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
