Demonstration purposes only

## Motivation

- With 2 machines, both behind different NAT/firewalls, proxy between them and reach the internal network without requiring a central server or exposing my IP address
- In censorship heavy countries (e.g. China, Iran, etc), double hop a VPN connection to relays located in friendlier countries to avoid suspicion while appearing as standard TLS
- Serve content to the outside world from within a NAT similar to ngrok.

## How it works

- syncthing but rather than files we send arbitrary data

## Usage

### Certificate Generation

Generate a certificate and key for use with other tools:

```bash
go run v2/cmd/generate-cert/main.go --output mykeys.gob --expiry 365
```

- `--output` is required, specifies the file to write the keypair.
- `--expiry` sets the number of days until expiry (default: 1095).

### SOCKS Browser

Start a local SOCKS5 proxy that uses Syncthing relays for .syncthing domains:

```bash
go run v2/cmd/socks-browser/main.go --keys mykeys.gob
```

- The proxy listens on `127.0.0.1:1080`.
- Use with browsers or tools that support SOCKS5.
- Connections to `*.syncthing` domains are routed via Syncthing relays.

### Reverse Proxy

Start a reverse proxy that exposes a local or remote HTTP service via Syncthing relay:

```bash
go run v2/cmd/reverse-proxy/main.go --target http://localhost:8080 --keys mykeys.gob --trusted trusted_ids.txt --country DE
```

- `--target` (required): The backend URL to proxy to.
- `--keys`: Path to gob-encoded keypair (use with `generate`).
- `--trusted`: File with trusted device IDs (one per line, optional).
- `--country`: Relay country code (optional, auto-detects if omitted).

## Examples

#### Serving an HTTP server over syncthing

```go
package main

import (
 "context"
 "log"
 "net"
 "net/http"

 "github.com/acheong08/syndicate/v2/lib"
 "github.com/acheong08/syndicate/v2/lib/crypto"
 "github.com/syncthing/syncthing/lib/protocol"
)

func main() {
 cert, _ := crypto.NewCertificate("syncthing", 1)
 log.Printf("Server ID: %s", protocol.NewDeviceID(cert.Certificate[0]))
 ctx, cancel := context.WithCancel(context.Background())
 defer cancel()

 mux := http.NewServeMux()
 mux.HandleFunc("/eggs", func(w http.ResponseWriter, r *http.Request) {
  if r.URL.Query().Get("q") != "magic" {
   w.WriteHeader(400)
  }
  w.Write([]byte("Hello world"))
 })
 connChan := make(chan net.Conn, 5)
 go lib.StartRelayManager(ctx, cert, []protocol.DeviceID{}, connChan, "")
 log.Fatal(lib.ServeMux(ctx, mux, connChan))
}
```

#### Fetching HTTP content over syncthing

You can use the `lib/hybridDialer` to create an `http.Client` that transparently fetches content from `.syncthing` domains via Syncthing relays. The dialer will automatically handle `.syncthing` domains.

```go
package main

import (
 "context"
 "crypto/tls"
 "io"
 "log"
 "net/http"
 "time"

 "github.com/acheong08/syndicate/v2/lib"
 "github.com/acheong08/syndicate/v2/lib/crypto"
)

func main() {
 cert, err := crypto.NewCertificate("syncthing", 1)
 if err != nil {
  log.Fatalf("failed to create certificate: %v", err)
 }

 dialer := lib.NewHybridDialer(cert)

 client := &http.Client{
  Transport: &http.Transport{
   DialContext: dialer.Dial,
  },
  Timeout: 15 * time.Second,
 }

 ctx := context.Background()
 req, err := http.NewRequestWithContext(ctx, "GET", "http://<deviceid>.syncthing/eggs?q=magic", nil)
 if err != nil {
  log.Fatalf("failed to create request: %v", err)
 }

 resp, err := client.Do(req)
 if err != nil {
  log.Fatalf("request failed: %v", err)
 }
 defer resp.Body.Close()

 body, _ := io.ReadAll(resp.Body)
 log.Printf("Status: %s\nBody: %s", resp.Status, string(body))
}
```
