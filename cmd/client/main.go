package main

import (
	"crypto/tls"
	_ "embed"

	"github.com/syncthing/syncthing/lib/protocol"
)

//go:embed certs/client.crt
var certPem []byte

//go:embed certs/client.key
var keyPem []byte

var serverDeviceID = "" // Override with `-ldflags "-X main.serverDeviceID=..."`

func main() {
	cert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		panic(err)
	}
	deviceID := protocol.NewDeviceID(cert.Certificate[0])
	println(deviceID.String())
	println(serverDeviceID)
}
