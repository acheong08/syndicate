package lib

import (
	"github.com/syncthing/syncthing/lib/protocol"
)

type ClientList []ClientEntry

type ClientEntry struct {
	Label      string
	ClientID   protocol.DeviceID
	ClientCert []byte // We need this for upgrading to TLS (RequireAndVerifyClientCert)
	ServerCert [][]byte
}

func (c ClientEntry) String() string {
	return c.Label
}
