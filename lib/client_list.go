package lib

import "github.com/syncthing/syncthing/lib/protocol"

type ClientList []ClientEntry

type ClientEntry struct {
	DeviceID   protocol.DeviceID
	ServerID   string
	ServerCert []byte
	ServerKey  []byte
}
