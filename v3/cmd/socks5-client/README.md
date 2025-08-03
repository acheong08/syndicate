# Syndicate SOCKS5 Client

This is a SOCKS5 proxy client that specifically handles `.syncthing` domains, routing them through the Syndicate transport mechanism to connect to other Syncthing devices.

## Features

- SOCKS5 proxy server that filters for `.syncthing` domains
- Automatic TLS certificate generation
- Routes `.syncthing` traffic through Syndicate transport
- Rejects non-`.syncthing` domains for security
- JSON configuration support
- Graceful shutdown

## Usage

### Basic Usage

```bash
# Run with default settings (listens on :1080, generates self-signed cert)
./socks5-client

# Specify listen address
./socks5-client -listen :1090

# Use existing TLS certificate  
./socks5-client -cert client.crt -key client.key
```

### Configuration File

Create a configuration file:

```json
{
  "listen_addr": ":1080",
  "cert_file": "client.crt", 
  "key_file": "client.key"
}
```

Then run:

```bash
./socks5-client -config config.json
```

## How It Works

1. The SOCKS5 client listens for proxy connections
2. When a client requests a connection to a `.syncthing` domain:
   - Parses the domain to extract the target device ID
   - Creates a connection via Syndicate transport to that device
   - Proxies data bidirectionally
3. Non-`.syncthing` domains are rejected for security

## Supported Domains

The proxy only accepts domains ending in `.syncthing`:

- `DEVICE-ID.syncthing` - Connect to device with ID `DEVICE-ID`
- `service.DEVICE-ID.syncthing` - Connect to specific service on device

For example:
- `ABC123-DEF456.syncthing:80` - HTTP to device ABC123-DEF456
- `api.ABC123-DEF456.syncthing:443` - HTTPS API to device ABC123-DEF456

## Client Configuration

Configure your applications to use the SOCKS5 proxy:

### curl
```bash
curl --socks5 localhost:1080 http://ABC123-DEF456.syncthing/health
```

### Browser
Set SOCKS5 proxy to `localhost:1080` in browser settings.

### Environment Variables
```bash
export HTTP_PROXY=socks5://localhost:1080
export HTTPS_PROXY=socks5://localhost:1080
```

## Command Line Options

- `-listen` - SOCKS5 listen address (default: ":1080")
- `-config` - Path to JSON configuration file
- `-cert` - TLS certificate file path
- `-key` - TLS private key file path

## Security

- Only `.syncthing` domains are allowed
- All other domains are rejected with SOCKS5 connection refused
- Uses TLS certificates for Syndicate authentication
- No authentication required for SOCKS5 (local use only)

## Notes

- If no certificate is provided, a self-signed certificate will be generated
- The Device ID is derived from the TLS certificate and used for Syndicate authentication
- Uses the Syncthing relay network for connectivity
- Supports graceful shutdown with SIGINT/SIGTERM