package lib

import (
	"github.com/syncthing/syncthing/lib/protocol"
)

type ClientList []ClientEntry

type ClientEntry struct {
	Label      string
	ClientID   protocol.DeviceID
	ClientCert []byte // We need this for upgrading to TLS (RequireAndVerifyClientCert)
	ServerID   string // This could be generated from the server cert/key but it's easier to just store it
	ServerCert [][]byte
}

func (c ClientEntry) String() string {
	return c.Label
}
