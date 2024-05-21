package relay

import (
	"encoding/json"
	"net/http"
	"net/url"
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
	RelayAddress  string
	DataAddresses []*url.URL
}

func (a AddressLister) ExternalAddresses() []string {
	addresses := make([]string, len(a.DataAddresses)+1)
	addresses[0] = a.RelayAddress
	for i, addr := range a.DataAddresses {
		addresses[i+1] = addr.String()
	}
	return addresses
}

func (a AddressLister) AllAddresses() []string {
	return a.ExternalAddresses()
}
