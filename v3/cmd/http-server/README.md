# Syndicate HTTP Server

This is an HTTP server that runs over the Syndicate transport mechanism, allowing other Syncthing devices to connect to it via .syncthing domains.

## Features

- Runs a standard HTTP server accessible via Syndicate transport
- Automatic TLS certificate generation 
- Health check, info, and echo endpoints
- JSON configuration support
- Graceful shutdown

## Usage

### Basic Usage

```bash
# Run with default settings (listens on :8080, generates self-signed cert)
./http-server

# Specify listen address
./http-server -listen :9090

# Use existing TLS certificate
./http-server -cert server.crt -key server.key
```

### Configuration File

Create a configuration file:

```json
{
  "listen_addr": ":8080",
  "device_id": "",
  "cert_file": "server.crt",
  "key_file": "server.key"
}
```

Then run:

```bash
./http-server -config config.json
```

## Endpoints

- `GET /` - Welcome page with links
- `GET /health` - Health check (JSON response)
- `GET /info` - Server information including device ID
- `GET /echo` - Echo request details

## Connection

Once running, the server will display its Device ID. Other devices can connect using:

```
http://DEVICE-ID.syncthing/
```

For example, if the Device ID is `ABC123-DEF456`, clients can access:
- `http://ABC123-DEF456.syncthing/health`
- `http://ABC123-DEF456.syncthing/info`

## Command Line Options

- `-listen` - HTTP listen address (default: ":8080")
- `-config` - Path to JSON configuration file
- `-cert` - TLS certificate file path
- `-key` - TLS private key file path

## Notes

- If no certificate is provided, a self-signed certificate will be generated
- The Device ID is derived from the TLS certificate
- The server uses the Syncthing relay network for connectivity
- Supports graceful shutdown with SIGINT/SIGTERM