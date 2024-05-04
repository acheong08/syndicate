package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"syndicate/lib/relay"
	"time"

	"github.com/syncthing/syncthing/lib/connections/registry"
	"github.com/syncthing/syncthing/lib/discover"
	"github.com/syncthing/syncthing/lib/events"
	syncthingprotocol "github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay/client"
	"github.com/syncthing/syncthing/lib/relay/protocol"
	"github.com/syncthing/syncthing/lib/tlsutil"
)

const SYNCTHING_DISCOVERY_URL = "https://discovery.syncthing.net/v2/?id=LYXKCHX-VI3NYZR-ALCJBHF-WMZYSPK-QG6QJA3-MPFYMSO-U56GTUK-NA2MIAW"

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: main serve/connect")
		return
	}
	switch os.Args[1] {
	case "serve":
		serve()
	case "connect":
		if len(os.Args) < 4 {
			fmt.Println("Usage: main connect <id> <relayURL>")
			return
		}
		connect(os.Args[2], os.Args[3])
	default:
		fmt.Println("Usage: main serve/connect")
	}
}

func connect(serverId string, relayURL string) {
	cert, _ := tlsutil.NewCertificateInMemory("syncthing1", 1)
	deviceID := syncthingprotocol.NewDeviceID(cert.Certificate[0])

	fmt.Println(deviceID.String())
	uri, err := url.Parse(relayURL)
	if err != nil {
		log.Fatalln(err)
	}
	ctx := context.Background()
	serverDeviceID, err := syncthingprotocol.DeviceIDFromString(serverId)
	if err != nil {
		log.Fatalln(err)
	}
	invite, err := client.GetInvitationFromRelay(ctx, uri, serverDeviceID, []tls.Certificate{cert}, time.Second*10)
	if err != nil {
		log.Fatalln(err)
	}
	conn, err := client.JoinSession(ctx, invite)
	if err != nil {
		log.Fatalln(err)
	}
	stdin := make(chan string)
	go stdinReader(stdin)
	log.Println("Connected to", conn.RemoteAddr(), conn.LocalAddr())
	connectToStdio(stdin, conn)
	log.Println("Disconnected")
}

func serve() {
	cert, _ := tlsutil.NewCertificateInMemory("syncthing", 1)
	myId := syncthingprotocol.NewDeviceID(cert.Certificate[0])
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
	log.Println("Using relay", addrURL.String())
	relay, err := client.NewClient(addrURL, []tls.Certificate{cert}, time.Second*10)
	if err != nil {
		fmt.Println(err)
		return
	}
	go relay.Serve(ctx)

	recv := make(chan protocol.SessionInvitation)

	go func() {
		for invite := range relay.Invitations() {
			// Prompt for a user ID
			log.Println("Received invite from", invite)
			select {
			case recv <- invite:
				log.Println("Sent invite to recv")
			default:
				log.Println("Discarded invite")
			}
		}
	}()
	stdin := make(chan string)
	go stdinReader(stdin)
	for {
		conn, err := client.JoinSession(ctx, <-recv)
		if err != nil {
			log.Fatalln(err)
		}
		log.Println("Joined", conn.RemoteAddr(), conn.LocalAddr())
		connectToStdio(stdin, conn)
		log.Println("Disconnected")

	}
}

func connectToStdio(stdin <-chan string, conn net.Conn) {
	buf := make([]byte, 1024)
	for {
		conn.SetReadDeadline(time.Now().Add(time.Millisecond))
		n, err := conn.Read(buf[0:])
		if err != nil {
			nerr, ok := err.(net.Error)
			if !ok || !nerr.Timeout() {
				log.Println(err)
				return
			}
		}
		os.Stdout.Write(buf[:n])

		select {
		case msg := <-stdin:
			_, err := conn.Write([]byte(msg))
			if err != nil {
				return
			}
		default:
		}
	}
}

func stdinReader(c chan<- string) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		c <- scanner.Text()
		c <- "\n"
	}
}
