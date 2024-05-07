package relay

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
)

func FetchRelays() (*Relays, error) {
	resp, err := http.Get("https://relays.syncthing.net/endpoint")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var relays Relays
	err = json.NewDecoder(resp.Body).Decode(&relays)
	if err != nil {
		return nil, err
	}

	return &relays, nil
}

type AddressLister struct {
	RelayAddress string
	IPs          []net.IP
	Ports        []uint16
}

func (a AddressLister) ExternalAddresses() []string {
	var addresses []string = make([]string, len(a.IPs)+1)
	addresses[0] = a.RelayAddress
	for i, ip := range a.IPs {
		addresses[i+1] = fmt.Sprintf("tcp://%s:%d", ip.String(), a.Ports[i])
	}
	return addresses
}

func (a AddressLister) AllAddresses() []string {
	return a.ExternalAddresses()
}
