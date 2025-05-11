package relay

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"
	"time"

	"github.com/rotisserie/eris"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/relay/client"
	relayprotocol "github.com/syncthing/syncthing/lib/relay/protocol"
)

var ErrCertificateNotMatch = eris.New("peer certificate does not match invite DeviceID")

func CreateSession(ctx context.Context, invite relayprotocol.SessionInvitation, cert tls.Certificate, serverName *string) (net.Conn, string, error) {
	rawConn, err := client.JoinSession(ctx, invite)
	if err != nil {
		return nil, "", eris.Wrap(err, "failed to create connection from invite")
	}
	tlsCfg := tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
		ClientAuth:         tls.RequireAnyClientCert,
	}
	var conn *tls.Conn
	if serverName == nil {
		conn = tls.Server(rawConn, &tlsCfg)
	} else {
		tlsCfg.ServerName = *serverName
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

const TIMEOUT = 30 * time.Second

func Listen(ctx context.Context, relayAddress string, cert tls.Certificate) (<-chan relayprotocol.SessionInvitation, error) {
	url, err := url.Parse(relayAddress)
	if err != nil {
		return nil, eris.Wrap(err, "failed to parse relay URL")
	}
	client, err := client.NewClient(url, []tls.Certificate{cert}, TIMEOUT)
	go client.Serve(ctx)
	return client.Invitations(), nil
}

func GetInvite(ctx context.Context, relayAddress string, deviceId protocol.DeviceID, cert tls.Certificate) (relayprotocol.SessionInvitation, error) {
	relayUrl, err := url.Parse(relayAddress)
	if err != nil {
		return relayprotocol.SessionInvitation{}, err
	}
	return client.GetInvitationFromRelay(ctx, relayUrl, deviceId, []tls.Certificate{cert}, TIMEOUT)
}
