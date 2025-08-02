package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/acheong08/syndicate/v2/internal"
	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
)

func main() {
	keysPath := flag.String("keys", "", "Path to gob encoded KeyPair")
	localAddr := flag.String("local", "127.0.0.1:1080", "Local address to listen on")
	serverID := flag.String("server", "", "Server device ID to connect to (required)")
	flag.Parse()

	if *serverID == "" {
		log.Fatal("The --server flag is required")
	}

	var cert tls.Certificate
	var err error
	if keysPath == nil || *keysPath == "" {
		cert, err = crypto.NewCertificate("socks-client", 1)
	} else {
		cert, err = internal.ReadKeyPair(*keysPath)
	}
	if err != nil {
		panic(err)
	}

	log.Printf("Starting SOCKS5 client on %s, connecting to server %s", *localAddr, *serverID)

	// Create hybrid dialer for syncthing connections
	dialer := lib.NewHybridDialer(cert)

	// Listen for local connections
	listener, err := net.Listen("tcp", *localAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", *localAddr, err)
	}
	defer listener.Close()

	log.Printf("SOCKS5 client listening on %s", *localAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		go handleConnection(conn, *serverID, dialer)
	}
}

type hybridDialer interface {
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
}

func handleConnection(localConn net.Conn, serverID string, dialer hybridDialer) {
	defer localConn.Close()

	// Connect to the SOCKS5 server through syncthing
	serverAddr := fmt.Sprintf("%s.syncthing:1080", serverID)
	remoteConn, err := dialer.Dial(context.Background(), "tcp", serverAddr)
	if err != nil {
		log.Printf("Failed to connect to server %s: %v", serverAddr, err)
		return
	}
	defer remoteConn.Close()

	// Tunnel the connection using io.Copy
	go func() {
		defer localConn.Close()
		defer remoteConn.Close()
		io.Copy(remoteConn, localConn)
	}()

	io.Copy(localConn, remoteConn)
}
