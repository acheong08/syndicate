package main

import (
	"context"
	"crypto/tls"
	_ "embed"
	"errors"
	"log"
	"net"
	"net/url"
	"time"

	"gitlab.torproject.org/acheong08/syndicate/lib"
	"gitlab.torproject.org/acheong08/syndicate/lib/commands"

	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/things-go/go-socks5"
)

//go:embed certs/client.crt
var certPem []byte

//go:embed certs/client.key
var keyPem []byte

var serverID = "" // Override with `-ldflags "-X main.serverID=..."`

var serverDeviceID protocol.DeviceID

var clientDeviceID protocol.DeviceID

const timeout = 20 * time.Second

var cert tls.Certificate

func init() {
	var err error
	serverDeviceID, err = protocol.DeviceIDFromString(serverID)
	if err != nil {
		panic(err)
	}
	cert, err = tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		panic(err)
	}
	log.SetFlags(log.Lshortfile)
}

func main() {
	clientDeviceID = protocol.NewDeviceID(cert.Certificate[0])
	syncthing, err := lib.NewSyncthing(context.Background(), cert, nil)
	if err != nil {
		panic(err)
	}
	for {
		err := func() error {
			relayAddress, err := getRelay(*syncthing)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				// We cancel in 10 seconds
				time.Sleep(time.Second * 10)
				cancel()
			}()
			conn, err := lib.ConnectToRelay(ctx, relayAddress, cert, serverDeviceID, time.Second*10, true)
			if err != nil {
				return err
			}
			log.Println("Connected to", conn.RemoteAddr())
			ctx, cancel = context.WithCancel(context.Background())
			defer cancel()
		cmdLoop:
			for {
				log.Println("Waiting for command")
				var command [1]byte
				_, err = conn.Read(command[:])
				if err != nil {
					return err
				}
				switch commands.Command(command[0]) {
				case commands.Socks5:
					log.Println("Starting Socks5 server with context")
					err := socksServer(ctx, (*relayAddress).String())
					if err != nil {
						log.Println("Socks error")
						return err
					}
				case commands.Exit:
					log.Println("Recieved exit command")
					break cmdLoop
				}
			}
			return nil
		}()
		if err != nil {
			log.Println(err)
			time.Sleep(time.Second * 10)
			continue
		}
		return

	}
}

func socksServer(ctx context.Context, relayAddress string) error {
	log.Println("Starting socks5 server")
	connChan := make(chan net.Conn)
	err := lib.ListenRelay(ctx, cert, relayAddress, serverDeviceID, nil, connChan)
	if err != nil {
		return err
	}
	socks5Server := socks5.NewServer()
	for {
		select {
		case conn := <-connChan:
			go func() {
				// Start a SOCKS5 server
				err := socks5Server.ServeConn(conn)
				if err != nil {
					log.Println(err)
				}
			}()
		case <-ctx.Done():
			return nil
		}
	}
}

func getRelay(syncthing lib.Syncthing) (relayAddress *url.URL, err error) {
	addresses, err := syncthing.Lookup(serverDeviceID)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		err = errors.New("no available addresses")
		return
	}
	if addresses[0].Scheme != "relay" {
		err = errors.New("first address is not a relay")
	}
	relayAddress = &addresses[0]
	return
}
