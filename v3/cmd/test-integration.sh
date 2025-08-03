#!/bin/bash

# Integration test script for Syndicate HTTP Server and SOCKS5 Client
# This script demonstrates how the components work together

set -e

echo "=== Syndicate Integration Test ==="

# Build both programs
echo "Building programs..."
go build -o http-server ./v3/cmd/http-server
go build -o socks5-client ./v3/cmd/socks5-client

# Start HTTP server in background
echo "Starting HTTP server..."
./http-server -listen :8080 &
HTTP_PID=$!

# Give server time to start
sleep 2

# Extract device ID from server logs
echo "Waiting for HTTP server to start..."
sleep 3

# Start SOCKS5 client in background  
echo "Starting SOCKS5 client..."
./socks5-client -listen :1080 &
SOCKS5_PID=$!

# Give SOCKS5 client time to start
sleep 2

echo "Both services started successfully!"
echo "HTTP Server PID: $HTTP_PID"
echo "SOCKS5 Client PID: $SOCKS5_PID"

echo ""
echo "=== Test Local HTTP Server ==="
echo "Testing direct connection to HTTP server..."
if curl -s http://localhost:8080/health | grep -q "healthy"; then
    echo "✓ HTTP server health check passed"
else
    echo "✗ HTTP server health check failed"
fi

echo ""
echo "=== Manual Testing Instructions ==="
echo "1. Check HTTP server device ID in the logs above"
echo "2. Test SOCKS5 proxy with:"
echo "   curl --socks5 localhost:1080 http://DEVICE-ID.syncthing/health"
echo "3. Test rejection of non-.syncthing domains:"
echo "   curl --socks5 localhost:1080 http://google.com/"

echo ""
echo "Press Ctrl+C to stop both services"

# Function to cleanup on exit
cleanup() {
    echo "Stopping services..."
    kill $HTTP_PID 2>/dev/null || true
    kill $SOCKS5_PID 2>/dev/null || true
    echo "Cleanup complete"
}

# Set up signal handlers
trap cleanup EXIT INT TERM

# Wait for user to stop
wait