package relay

import (
	"encoding/json"
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
}

func (a AddressLister) ExternalAddresses() []string {
	return []string{a.RelayAddress}
}

func (a AddressLister) AllAddresses() []string {
	return a.ExternalAddresses()
}
