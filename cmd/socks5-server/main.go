package main

import (
	"context"
	"log"
	"net"
	"net/url"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/tlsutil"
	"gitlab.torproject.org/acheong08/syndicate/lib"
)

func main() {
	cert, _ := tlsutil.NewCertificateInMemory("socks5-server", 1)
	deviceID := protocol.NewDeviceID(cert.Certificate[0])
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	relayAddress, err := lib.FindOptimalRelay("DE")
	if err != nil {
		panic(err)
	}
	log.Println("Starting socks server at", relayAddress, "with deviceID", deviceID.String())
	go func() {
		err := lib.StartSocksServer(ctx, relayAddress, cert, deviceID)
		if err != nil {
			panic(err)
		}
	}()
	time.Sleep(2 * time.Second)
	listener, _ := net.Listen("tcp", "127.0.0.1:1070")
	for {
		socksConn, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		relayURL, _ := url.Parse(relayAddress)
		// Generate a new deviceID/certificate
		// sockCert, _ := tlsutil.NewCertificateInMemory("socks5-client", 1)
		go lib.HandleSocks(relayURL, socksConn, deviceID, cert)
	}
}
