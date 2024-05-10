package main

import (
	"context"
	"crypto/tls"
	_ "embed"
	"errors"
	"log"
	"net"
	"net/url"
	"gitlab.torproject.org/acheong08/syndicate/lib"
	"gitlab.torproject.org/acheong08/syndicate/lib/utils"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay/client"
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
	syncthing, err := lib.NewSyncthing(cert, nil)
	if err != nil {
		panic(err)
	}
	for {
		relayAddress, err := getRelay(*syncthing)
		if err != nil {
			time.Sleep(timeout)
			continue
		}
		_, err = ConnectToRelay(relayAddress)
		if err != nil {
			log.Println(err.Error())
			time.Sleep(timeout)
			continue
		}
		// TODO: Do something with the connection
		return

	}
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
