# Syndicate v3 Command Line Tools

This directory contains command-line tools that demonstrate the Syndicate v3 transport mechanism for secure, peer-to-peer networking using Syncthing's infrastructure.

## Components

### HTTP Server (`http-server/`)

A web server that runs over the Syndicate transport, making itself accessible to other Syncthing devices via `.syncthing` domains.

**Features:**
- HTTP server accessible via `DEVICE-ID.syncthing` domains
- Health, info, and echo endpoints
- Automatic TLS certificate generation
- JSON configuration support

**Usage:**
```bash
./http-server -listen :8080
```

### SOCKS5 Client (`socks5-client/`)

A SOCKS5 proxy that routes traffic to `.syncthing` domains through the Syndicate transport mechanism.

**Features:**
- SOCKS5 proxy server for `.syncthing` domains only
- Automatic device ID extraction from domains
- Security filtering (rejects non-`.syncthing` traffic)
- Bidirectional data proxying

**Usage:**
```bash
./socks5-client -listen :1080
```

## How It Works

1. **HTTP Server** creates a syndicate server that listens for connections from other devices
2. **SOCKS5 Client** creates a syndicate client that can connect to other devices
3. Communication happens through Syncthing's relay network using device certificates
4. Domains use the format `DEVICE-ID.syncthing` where `DEVICE-ID` is derived from TLS certificates

## Example Workflow

1. Start HTTP server:
   ```bash
   ./http-server
   # Note the displayed device ID, e.g., ABC123-DEF456
   ```

2. Start SOCKS5 client:
   ```bash
   ./socks5-client
   ```

3. Access the HTTP server through the proxy:
   ```bash
   curl --socks5 localhost:1080 http://ABC123-DEF456.syncthing/health
   ```

## Architecture

```
Client App → SOCKS5 Proxy → Syndicate Transport → Relay Network → Target Device
```

- **Client App**: Any application that supports SOCKS5 proxies
- **SOCKS5 Proxy**: Filters and routes `.syncthing` domains
- **Syndicate Transport**: Handles device authentication and connection multiplexing
- **Relay Network**: Syncthing's relay infrastructure for NAT traversal
- **Target Device**: HTTP server or other service running on a Syncthing device

## Security

- All connections use TLS with device certificate authentication
- SOCKS5 proxy only allows `.syncthing` domains (security filter)
- Device IDs are cryptographically derived from certificates
- Uses Syncthing's trusted relay network infrastructure

## Build & Test

```bash
# Build both programs
go build ./v3/cmd/http-server
go build ./v3/cmd/socks5-client

# Run integration test
./v3/cmd/test-integration.sh
```

## Configuration

Both programs support:
- Command-line flags for basic options
- JSON configuration files for advanced settings
- Automatic TLS certificate generation
- Graceful shutdown handling