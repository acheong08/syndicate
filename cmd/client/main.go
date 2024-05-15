package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"log"
	"net"
	"net/url"
	"time"

	"gitlab.torproject.org/acheong08/syndicate/lib"
	"gitlab.torproject.org/acheong08/syndicate/lib/commands"
	"gitlab.torproject.org/acheong08/syndicate/lib/utils"

	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay/client"
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
			conn, err := ConnectToRelay(relayAddress)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
		cmdLoop:
			for {
				var command [1]byte
				conn.Read(command[:])
				switch commands.Command(command[0]) {
				case commands.Socks5:
					log.Println("Starting Socks5 server with context")
					err := socksServer(ctx, (*relayAddress).String())
					if err != nil {
						return err
					}
				case commands.Exit:
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
	clientCert, err := x509.ParseCertificate(certPem)
	if err != nil {
		return err
	}
	connChan := make(chan net.Conn)
	err = lib.ListenRelay(ctx, cert, relayAddress, clientDeviceID, clientCert, connChan)
	if err != nil {
		return err
	}
	socks5Server := socks5.NewServer()
	go func() {
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
				return
			}
		}
	}()
	return nil
}

func ConnectToRelay(relayAddress *url.URL) (net.Conn, error) {
	ctx := context.Background()

	invite, err := client.GetInvitationFromRelay(ctx, relayAddress, serverDeviceID, []tls.Certificate{cert}, timeout)
	if err != nil {
		return nil, err
	}

	conn, err := client.JoinSession(ctx, invite)
	if err != nil {
		return nil, err
	}
	return utils.UpgradeClientConn(conn, cert, time.Second*5)
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
