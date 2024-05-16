package main

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
	"gitlab.torproject.org/acheong08/syndicate/lib"
)

func handleSocks(relayAddress *url.URL, socksConn net.Conn, deviceID protocol.DeviceID, cert tls.Certificate) {
	log.Println("Got socks connection")
	defer socksConn.Close()
	// Connect to relay
	relayConn, err := lib.ConnectToRelay(context.Background(), relayAddress, cert, deviceID, time.Second*5, false)
	if err != nil {
		return
	}
	defer relayConn.Close()
	// Copy/Connect local socks connection and relay connection
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(relayConn, socksConn)
	}()
	go func() {
		defer wg.Done()
		io.Copy(socksConn, relayConn)
	}()
	wg.Wait()
}
