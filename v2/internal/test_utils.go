package internal

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/acheong08/syndicate/v2/lib/relay"
	"github.com/syncthing/syncthing/lib/protocol"
	relayprotocol "github.com/syncthing/syncthing/lib/relay/protocol"
)

func TryGetInviteUntil(ctx context.Context, relayURL string, serverDeviceId protocol.DeviceID, clientCert tls.Certificate, timeout time.Duration) (relayprotocol.SessionInvitation, error) {
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
