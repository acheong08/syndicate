package lib_test

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/acheong08/syndicate/v2/lib"
	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/acheong08/syndicate/v2/lib/relay"
	"github.com/syncthing/syncthing/lib/protocol"
	relayprotocol "github.com/syncthing/syncthing/lib/relay/protocol"
)

func TestHttpServing(t *testing.T) {
	serverCert, _ := crypto.NewCertificate("syncthing", 1)
	serverDeviceId := protocol.NewDeviceID(serverCert.Certificate[0])
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relays, err := relay.FindOptimal(ctx, "DE", 1)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/eggs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "magic" {
			w.WriteHeader(400)
		}
		w.Write([]byte("Hello world"))
	})
	invites, err := relay.Listen(ctx, relays.First().URL, serverCert)
	if err != nil {
		t.Fatal(err)
	}
	log.Println("Server recieved invite")
	go func() {
		connChan := make(chan net.Conn)
		go func() {
			if err := lib.ServeMux(ctx, mux, connChan); err != nil {
				panic(err)
			}
		}()
		for {
			select {
			case invite := <-invites:
				serverConn, _, err := relay.CreateSession(ctx, invite, serverCert, nil)
				if err != nil {
					panic(err)
				}
				defer serverConn.Close()
				connChan <- serverConn
			case <-ctx.Done():
				return
			}
		}
	}()

	log.Println("Server session created")
	clientCert, _ := crypto.NewCertificate("syncthing", 1)
	invite, err := tryGetInviteUntil(ctx, relays.First().URL, serverDeviceId, clientCert, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	conn, _, err := relay.CreateSession(ctx, invite, clientCert, &empty)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	log.Println("Client session created")
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/eggs?q=magic", nil)
	if err = req.Write(conn); err != nil {
		t.Fatal(err)
	}
	log.Println("Client request sent")
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	log.Println("Client received response")
	if resp.StatusCode != 200 {
		t.Fatalf("Got status code %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	log.Println(string(body))
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
