package main

import (
	"encoding/gob"
	"os"

	"github.com/syncthing/syncthing/lib/protocol"
)

func main() {
	clientList := getClientList()
	for _, client := range clientList {
		println(client.String())
	}
}

func getClientList() []protocol.DeviceID {
	var clientList []protocol.DeviceID
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
