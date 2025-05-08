package lib

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/rotisserie/eris"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/things-go/go-socks5"
)

func StartSocksServer(ctx context.Context, relayAddress string, cert tls.Certificate, clientDeviceID protocol.DeviceID) error {
	log.Println("Starting socks5 server")
	connChan := make(chan net.Conn)
	err := ListenRelay(ctx, cert, relayAddress, &clientDeviceID, connChan)
	if err != nil {
		return eris.Wrap(err, "Could not start socks server due to relay")
	}
	socks5Server := socks5.NewServer()
	for {
		select {
		case conn := <-connChan:
			log.Println("Got socks connection", conn.RemoteAddr())
			go func() {
				// Start a SOCKS5 server
				err := socks5Server.ServeConn(conn)
				if err != nil {
					log.Println(err)
				}
			}()
		case <-ctx.Done():
			log.Println("Socks server cancelled by context")
			return nil
		}
	}
}

func HandleSocks(relayAddress *url.URL, socksConn net.Conn, deviceID protocol.DeviceID, cert tls.Certificate) error {
	log.Println("Got socks connection")
	defer socksConn.Close()
	// Connect to relay
	relayConn, err := ConnectToRelay(context.Background(), relayAddress, cert, deviceID, time.Second*5)
	if err != nil {
		return eris.Wrap(err, "failed to connect to relay")
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
	return nil
}
