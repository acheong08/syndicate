package lib

import (
	"crypto/tls"

	"github.com/syncthing/syncthing/lib/protocol"
)

type ClientList []ClientEntry

type ClientEntry struct {
	ClientID   protocol.DeviceID
	ClientCert tls.Certificate // We need this for upgrading to TLS (RequireAndVerifyClientCert)
	ServerID   string          // This could be generated from the server cert/key but it's easier to just store it
	ServerCert []byte
	ServerKey  []byte
}
