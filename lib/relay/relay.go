package relay

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"

	"github.com/rotisserie/eris"
)

func FetchRelays() (*Relays, error) {
	resp, err := http.Get("https://relays.syncthing.net/endpoint")
	if err != nil {
		return nil, eris.Wrap(err, "failed to fetch relays endpoint")
	}
	defer resp.Body.Close()

	var relays Relays
	err = json.NewDecoder(resp.Body).Decode(&relays)
	if err != nil {
		return nil, eris.Wrap(err, "Could not decode relays as JSON")
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
	log.Println("Broadcasting addresses:", addresses)
	return addresses
}

func (a AddressLister) AllAddresses() []string {
	return a.ExternalAddresses()
}
