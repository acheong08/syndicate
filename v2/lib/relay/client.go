package relay

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/rotisserie/eris"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay/client"
	relayprotocol "github.com/syncthing/syncthing/lib/relay/protocol"
)

var ErrCertificateNotMatch = eris.New("peer certificate does not match invite DeviceID")

func CreateSession(ctx context.Context, invite relayprotocol.SessionInvitation, cert tls.Certificate, server bool) (net.Conn, string, error) {
	rawConn, err := client.JoinSession(ctx, invite)
	if err != nil {
		return nil, "", eris.Wrap(err, "failed to create connection from invite")
	}
	tlsCfg := tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	var conn *tls.Conn
	if server {
		conn = tls.Server(rawConn, &tlsCfg)
	} else {
		conn = tls.Client(rawConn, &tlsCfg)
	}
	if err := conn.Handshake(); err != nil {
		return nil, "", eris.Wrap(err, "tls handshake failed")
	}
	peerDeviceID := protocol.NewDeviceID(conn.ConnectionState().PeerCertificates[0].Raw)
	if peerDeviceID != protocol.DeviceID(invite.From) {
		conn.Close()
		return nil, "", ErrCertificateNotMatch
	}
	sni := conn.ConnectionState().ServerName
	return conn, sni, nil
}
