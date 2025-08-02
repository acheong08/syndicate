# Syndicate

A peer-to-peer networking tool that enables secure communication between devices behind NAT/firewalls using the Syncthing relay network.

**Note**: This project is for demonstration purposes only.

## Motivation

- **NAT Traversal**: Connect 2 machines behind different NAT/firewalls and proxy between them to reach internal networks without requiring a central server or exposing IP addresses
- **Censorship Resistance**: In restrictive countries, double-hop VPN connections through relays in friendlier countries while appearing as standard TLS traffic
- **Reverse Proxy**: Serve content to the outside world from within a NAT, similar to ngrok

## How it works

Uses the Syncthing relay network infrastructure but for arbitrary data instead of file synchronization. The current implementation (v2) provides:

- **Relay Management**: Automated discovery and connection to Syncthing relays
- **Hybrid Dialer**: HTTP client support with automatic routing for `.syncthing` domains
- **SOCKS Support**: SOCKS5 proxy functionality for browser integration
- **SNI Routing**: Multiple subdomain support for reverse proxy scenarios

## Breaking Changes

**⚠️ Version 2.0**: The v1 implementation has been completely removed. All commands now use the v2 architecture located in the `v2/` directory.

## Available Commands

### Certificate Generation

Generate a certificate and key for use with other tools:

```bash
go run v2/cmd/generate-cert/main.go --output mykeys.gob --expiry 365
```

- `--output` is required, specifies the file to write the keypair
- `--expiry` sets the number of days until expiry (default: 1095)

### SOCKS Browser

Start a local SOCKS5 proxy that routes `.syncthing` domains through Syncthing relays:

```bash
go run v2/cmd/socks-browser/main.go --keys mykeys.gob
```

- The proxy listens on `127.0.0.1:1080`
- Use with browsers or tools that support SOCKS5
- Connections to `*.syncthing` domains are routed via Syncthing relays

### SOCKS Client/Server

Start a SOCKS client or server for peer-to-peer connections:

```bash
# SOCKS Server
go run v2/cmd/socks-server/main.go --keys mykeys.gob --trusted trusted_ids.txt

# SOCKS Client  
go run v2/cmd/socks-client/main.go --keys mykeys.gob --server <device-id>
```

### Reverse Proxy

Start a reverse proxy that exposes a local or remote HTTP service via Syncthing relay:

```bash
go run v2/cmd/reverse-proxy/main.go --target http://localhost:8080 --keys mykeys.gob --trusted trusted_ids.txt --country DE
```

- `--target` (required): The backend URL to proxy to
- `--keys`: Path to gob-encoded keypair (use with `generate-cert`)
- `--trusted`: File with trusted device IDs (one per line, optional)
- `--country`: Relay country code (optional, auto-detects if omitted)

### HTTP Hello Server

Basic HTTP server example for testing:

```bash
go run v2/cmd/http-hello/main.go --keys mykeys.gob --trusted trusted_ids.txt
```

## Library Usage

### HTTP Client with Hybrid Dialer

Create an HTTP client that automatically routes `.syncthing` domains through Syncthing relays:

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
    req, err := http.NewRequestWithContext(ctx, "GET", "http://<deviceid>.syncthing/api/status", nil)
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

### HTTP Server with SNI Routing

Serve HTTP content over Syncthing relays with multiple subdomain support:

```go
package main

import (
    "context"
    "log"
    "net"
    "net/http"
    "sync"

    "github.com/acheong08/syndicate/v2/lib"
    "github.com/acheong08/syndicate/v2/lib/crypto"
    "github.com/syncthing/syncthing/lib/protocol"
)

func main() {
    cert, _ := crypto.NewCertificate("syncthing", 1)
    log.Printf("Server Device ID: %s", protocol.NewDeviceID(cert.Certificate[0]))
    
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Create multiple HTTP handlers
    handler1 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Hello from service 1"))
    })
    handler2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Hello from service 2"))
    })

    // Start relay manager
    relayChan := lib.StartRelayManager(ctx, cert, []protocol.DeviceID{}, "DE")
    
    // Create channels for different services
    service1Chan := make(chan net.Conn, 5)
    service2Chan := make(chan net.Conn, 5)
    
    // Route connections based on SNI
    go func() {
        for {
            select {
            case <-ctx.Done():
                return
            case relayOut := <-relayChan:
                switch relayOut.Sni {
                case "service1":
                    service1Chan <- relayOut.Conn
                case "service2":
                    service2Chan <- relayOut.Conn
                default:
                    relayOut.Conn.Close()
                }
            }
        }
    }()

    // Serve multiple services concurrently
    var wg sync.WaitGroup
    wg.Add(2)
    
    go func() {
        defer wg.Done()
        log.Fatal(lib.ServeHTTP(ctx, handler1, service1Chan))
    }()
    
    go func() {
        defer wg.Done()
        log.Fatal(lib.ServeHTTP(ctx, handler2, service2Chan))
    }()
    
    wg.Wait()
}
```
