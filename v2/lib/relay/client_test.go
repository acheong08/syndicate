package relay_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/acheong08/syndicate/v2/lib/relay"
	"github.com/syncthing/syncthing/lib/protocol"
	relayprotocol "github.com/syncthing/syncthing/lib/relay/protocol"
)

var magicSni = "test.relay"

var magicBytes = []byte{0xde, 0xad, 0xba, 0xbe}

func TestRelayConnection(t *testing.T) {
	relays, err := relay.FindOptimal("DE")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	serverCert, _ := crypto.NewCertificate(magicSni, 1)
	invites, err := relay.Listen(ctx, relays.First().URL, serverCert)
	if err != nil {
		t.Fatal(err)
	}
	handleConn := func(conn net.Conn) {
		conn.Write(magicBytes)
		var resp [4]byte
		_, err = conn.Read(resp[:])
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(magicBytes, resp[:]) {
			t.Fatalf("magic bytes not matched: expected %v found %v", magicBytes, resp)
		}
	}
	go func(invites <-chan relayprotocol.SessionInvitation) {
		for inv := range invites {
			conn, sni, err := relay.CreateSession(ctx, inv, serverCert, nil)
			if err != nil {
				panic(err)
			}
			defer conn.Close()
			if sni != magicSni {
				panic(fmt.Sprintf("SNI: expected %s found %s", magicSni, sni))
			}
			handleConn(conn)

			break
		}
	}(invites)
	clientCert, _ := crypto.NewCertificate(magicSni, 1)
	serverDeviceId := protocol.NewDeviceID(serverCert.Certificate[0])
	invite, err := tryGetInviteUntil(ctx, relays.First().URL, serverDeviceId, clientCert, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := relay.CreateSession(ctx, invite, clientCert, &magicSni)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// if sni != magicSni {
	// 	t.Fatalf("SNI: expected %s found %s", magicSni, sni)
	// }
	handleConn(conn)
}
func tryGetInviteUntil(ctx context.Context, relayURL string, serverDeviceId protocol.DeviceID, clientCert tls.Certificate, timeout time.Duration) (relayprotocol.SessionInvitation, error) {
	deadline := time.Now().Add(timeout)
	var invite relayprotocol.SessionInvitation
	var err error
	for {
		invite, err = relay.GetInvite(ctx, relayURL, serverDeviceId, clientCert)
		if err == nil {
			return invite, nil
		}
		if time.Now().After(deadline) {
			return relayprotocol.SessionInvitation{}, fmt.Errorf("timeout waiting for invite: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
