package relay_test

import (
	"crypto/tls"
	"testing"

	"github.com/acheong08/syndicate/v2/lib/crypto"
	"github.com/syncthing/syncthing/lib/protocol"
)

func TestDeviceIdIdentical(t *testing.T) {
	var clientSeenDevice protocol.DeviceID
	var serverSeenDevice protocol.DeviceID

	cert, err := crypto.NewCertificate("syncthing", 1)
	if err != nil {
		t.Error(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
	})
	if err != nil {
		t.Error(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Error(err)
		}
		tlsConn := conn.(*tls.Conn)
		if err := tlsConn.Handshake(); err != nil {
			t.Error(err)
		}
		state := tlsConn.ConnectionState()
		serverSeenDevice = protocol.NewDeviceID(state.PeerCertificates[0].Raw)
		conn.Close()
		close(done)
	}()

	clientCert, err := crypto.NewCertificate("syncthing", 1)
	if err != nil {
		t.Error(err)
	}
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		Certificates:       []tls.Certificate{clientCert},
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Error(err)
	}
	clientSeenDevice = protocol.NewDeviceID(clientCert.Certificate[0])
	conn.Close()
	<-done

	if !clientSeenDevice.Equals(serverSeenDevice) {
		t.Errorf("client and server seen certificate differs: %s - %s", clientSeenDevice.Short(), serverSeenDevice.Short())
	}
}
