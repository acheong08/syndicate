package config

import (
	"crypto/tls"

	"github.com/rotisserie/eris"
	"github.com/syncthing/syncthing/lib/protocol"
)

type ClientConfig struct {
	MasterDeviceID protocol.DeviceID
	Certificate    tls.Certificate
	DeviceID       protocol.DeviceID
}

type clientConfig struct {
	MasterDeviceID string // protocol.DeviceID
	KeyPair        keyPair
}
type keyPair struct {
	certPem string
	keyPem  string
}

func (g clientConfig) Parse() (gc ClientConfig) {
	var err error
	gc.MasterDeviceID, err = protocol.DeviceIDFromString(g.MasterDeviceID)
	if err != nil {
		panic(eris.Wrap(err, "failed to parse master device id"))
	}

	gc.Certificate, err = tls.X509KeyPair([]byte(g.KeyPair.certPem), []byte(g.KeyPair.keyPem))
	if err != nil {
		panic(eris.Wrap(err, "invalid tls keypairs"))
	}
	gc.DeviceID = protocol.NewDeviceID(gc.Certificate.Certificate[0])
	return
}

// type Endpoints struct {
// 	RelayStat         string
// 	DiscoveryLookup   string
// 	DiscoveryAnnounce string
// }
