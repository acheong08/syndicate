package main

import (
	"context"
	"crypto/tls"
	"flag"
	"io"
	"log"
	"net"
	"time"

	"github.com/acheong08/syndicate/v2/internal"
	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/acheong08/syndicate/v2/lib/mux"
	"github.com/syncthing/syncthing/lib/protocol"
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

	deviceID, err := protocol.DeviceIDFromString(*serverID)
	if err != nil {
		log.Fatalf("Invalid server device ID: %v", err)
	}

	log.Printf("Starting SOCKS5 client on %s, connecting to server %s", *localAddr, *serverID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create hybrid dialer for syncthing connections
	hybridDialer := lib.NewHybridDialer(cert)

	// Create tunnel manager for efficient connection reuse
	tunnelManager := mux.NewTunnelManager(cert, hybridDialer,
		mux.WithMaxPoolSize(5),
		mux.WithMaxIdleTime(300*time.Second),
	)
	defer tunnelManager.Close()

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

		go handleConnection(ctx, conn, deviceID, tunnelManager)
	}
}

func handleConnection(ctx context.Context, localConn net.Conn, serverDeviceID protocol.DeviceID, tunnelManager *mux.TunnelManager) {
	defer localConn.Close()

	// Get a multiplexed connection to the server
	remoteConn, err := tunnelManager.GetConnection(ctx, serverDeviceID)
	if err != nil {
		log.Printf("Failed to get connection to server %s: %v", serverDeviceID.Short(), err)
		return
	}
	defer remoteConn.Close()

	log.Printf("Established multiplexed connection for local client %s", localConn.RemoteAddr())

	// Tunnel the connection using io.Copy in both directions
	done := make(chan struct{}, 2)

	// Copy from local to remote
	go func() {
		defer func() { done <- struct{}{} }()
		io.Copy(remoteConn, localConn)
		remoteConn.Close()
	}()

	// Copy from remote to local
	go func() {
		defer func() { done <- struct{}{} }()
		io.Copy(localConn, remoteConn)
		localConn.Close()
	}()

	// Wait for either direction to complete
	<-done
}
