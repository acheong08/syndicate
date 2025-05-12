package relay_test

import (
	"context"
	"testing"
	"time"

	"github.com/acheong08/syndicate/v2/lib/relay"
)

func TestRelays_Filter(t *testing.T) {
	relays := relay.Relays{
		Relays: []relay.Relay{
			{Location: relay.Location{Country: "US"}},
			{Location: relay.Location{Country: "DE"}},
		},
	}
	relays.Filter(func(r relay.Relay) bool { return r.Location.Country == "DE" })
	if len(relays.Relays) != 1 || relays.Relays[0].Location.Country != "DE" {
		t.Errorf("Filter failed, got %+v", relays.Relays)
	}
}

func TestRelays_Sort(t *testing.T) {
	relays := relay.Relays{
		Relays: []relay.Relay{
			{Stats: relay.Stats{NumActiveSessions: 1}},
			{Stats: relay.Stats{NumActiveSessions: 3}},
			{Stats: relay.Stats{NumActiveSessions: 2}},
		},
	}
	relays.Sort(func(a, b relay.Relay) bool {
		return a.Stats.NumActiveSessions > b.Stats.NumActiveSessions
	}, false)
	if relays.Relays[0].Stats.NumActiveSessions != 3 {
		t.Errorf("Sort failed, got %+v", relays.Relays)
	}
}

func TestList(t *testing.T) {
	relays, err := relay.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(relays.Relays) == 0 {
		t.Errorf("List() got %+v", relays.Relays)
	}
}
func TestOptimalRelays(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	relays, err := relay.FindOptimal(ctx, "DE", 10)
	if err != nil {
		t.Fatalf("FindOptimal() error: %v", err)
	}
	if len(relays.Relays) == 0 {
		t.Errorf("FindOptimal() got %+v", relays.Relays)
	}
	for _, r := range relays.Relays {
		if r.Location.Country != "DE" {
			t.Errorf("FindOptimal() did not correctly filter for country DE")
		}
	}
}
