package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"

	"github.com/acheong08/syndicate/v2/internal"
	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/things-go/go-socks5"
)

func main() {
	keysPath := flag.String("keys", "", "Path to gob encoded KeyPair")
	flag.Parse()
	var cert tls.Certificate
	var err error
	if keysPath == nil || *keysPath == "" {
		cert, err = crypto.NewCertificate("syncthing-client", 1)
	} else {
		cert, err = internal.ReadKeyPair(*keysPath)
	}
	if err != nil {
		panic(err)
	}
	log.Printf("Starting socks with ID %s", protocol.NewDeviceID(cert.Certificate[0]))

	dialer := lib.NewHybridDialer(cert)
	server := socks5.NewServer(socks5.WithDial(dialer.Dial), socks5.WithResolver(lib.DNSResolver{}))

	// Start server
	fmt.Println("Starting SOCKS5 server on :1080")
	if err := server.ListenAndServe("tcp", "127.0.0.1:1080"); err != nil {
		panic(err)
	}
}
