package main

import (
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/binary"
	"errors"
	"log"
	"time"

	"gitlab.torproject.org/acheong08/syndicate/lib"
	"gitlab.torproject.org/acheong08/syndicate/lib/commands"
	"gitlab.torproject.org/acheong08/syndicate/lib/utils"

	"github.com/syncthing/syncthing/lib/protocol"
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
	var insID uint32
	jobs := make(map[commands.Command]context.CancelFunc)
	for {
		err := func() error {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			syncthing, err := lib.NewSyncthing(ctx, cert, nil)
			if err != nil {
				return err
			}
			addresses, err := syncthing.Lookup(serverDeviceID)
			if err != nil {
				return err
			}
			relayAddress := addresses[0]
			data, err := utils.DecodeURLs(addresses[1:])
			if err != nil {
				return err
			}
			if len(data) < 5 {
				return errors.New("recieved insufficient data")
			}
			command := commands.Command(data[0])
			commandID := binary.BigEndian.Uint32(data[1:5])
			if commandID == insID {
				return nil
			}
			insID = commandID

			switch command {
			case commands.StartSocks5:
				{
					if _, ok := jobs[command]; ok {
						jobs[command]()
						delete(jobs, command)
					}
					ctx, cancel := context.WithCancel(context.Background())
					go lib.StartSocksServer(ctx, relayAddress.String(), cert, serverDeviceID)
					jobs[command] = cancel
				}
			case commands.StopSocks5:
				if cancel, ok := jobs[command]; ok {
					cancel()
					delete(jobs, command)
				}
			}
			return nil
		}()
		if err != nil {
			log.Println(err)
		}
		time.Sleep(time.Second * 10)
		continue

	}
}
