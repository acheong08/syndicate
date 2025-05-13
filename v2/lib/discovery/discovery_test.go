package discovery_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/acheong08/syndicate/v2/lib/discovery"
	"github.com/syncthing/syncthing/lib/protocol"
)

const magicAddress = "tcp://172.20.10.14:2388"

func TestBroadcastLookup(t *testing.T) {
	cert, _ := crypto.NewCertificate("syncthing", 1)
	deviceId := protocol.NewDeviceID(cert.Certificate[0])
	endpoint := discovery.GetDiscoEndpoint(discovery.OptDiscoEndpointAuto)
	lister := discovery.NewAddressLister()
	lister.UpdateAddresses([]string{magicAddress})

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Minute))
	defer cancel()
	go func() {
		log.Printf("Broadcasting address: %s", magicAddress)
		if err := discovery.Broadcast(ctx, cert, &lister, endpoint); err != nil {
			if err.Error() != "context canceled" {
				panic(err)
			}
		}
	}()
	for {
		time.Sleep(2 * time.Second)
		select {
		case <-ctx.Done():
			t.Fatal("unable to find broadcasted device")
			return
		default:
			log.Println("Starting device lookup")
			result, err := discovery.LookupDevice(ctx, deviceId, endpoint)
			if err != nil {
				log.Printf("Failed to find device: %s, retrying...", err)
				continue
			}
			if len(result) != 1 {
				t.Fatalf("length of address list not match: %v", result)
			}
			if result[0] != magicAddress {
				t.Fatalf("Expected %s, got %s for magic address", magicAddress, result[0])
			}
			log.Printf("Found address %s", result[0])
			return
		}
	}
}
