package main

import (
	"encoding/gob"
	"os"
	"syndicate/lib"
)

func main() {
	clientList := getClientList()
	for _, client := range clientList {
		println(client.DeviceID.String(), client.ServerID)
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
