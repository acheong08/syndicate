package mux

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestSessionBasic(t *testing.T) {
	conn1, conn2 := mockPipe()
	defer conn1.Close()
	defer conn2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create client and server sessions
	clientSession := NewClientSession(ctx, conn1)
	serverSession := NewServerSession(ctx, conn2)

	defer clientSession.Close()
	defer serverSession.Close()

	// Client opens a stream
	clientStream, err := clientSession.OpenStream()
	if err != nil {
		t.Fatalf("Failed to open client stream: %v", err)
	}
	defer clientStream.Close()

	// Server accepts the stream
	serverStream, err := serverSession.AcceptStream()
	if err != nil {
		t.Fatalf("Failed to accept server stream: %v", err)
	}
	defer serverStream.Close()

	// Test data exchange
	testData := []byte("Hello, multiplexed world!")

	// Client writes data
	n, err := clientStream.Write(testData)
	if err != nil {
		t.Fatalf("Client write failed: %v", err)
	}
	if n != len(testData) {
		t.Fatalf("Client write incomplete: wrote %d, expected %d", n, len(testData))
	}

	// Server reads data
	readData := make([]byte, len(testData))
	n, err = io.ReadFull(serverStream, readData)
	if err != nil {
		t.Fatalf("Server read failed: %v", err)
	}
	if n != len(testData) {
		t.Fatalf("Server read incomplete: read %d, expected %d", n, len(testData))
	}

	// Verify data
	if string(readData) != string(testData) {
		t.Fatalf("Data mismatch: got %q, expected %q", string(readData), string(testData))
	}

	// Test reverse direction
	responseData := []byte("Response from server")

	// Server writes response
	n, err = serverStream.Write(responseData)
	if err != nil {
		t.Fatalf("Server write failed: %v", err)
	}

	// Client reads response
	responseRead := make([]byte, len(responseData))
	n, err = io.ReadFull(clientStream, responseRead)
	if err != nil {
		t.Fatalf("Client read failed: %v", err)
	}

	// Verify response
	if string(responseRead) != string(responseData) {
		t.Fatalf("Response mismatch: got %q, expected %q", string(responseRead), string(responseData))
	}
}

func TestSessionMultipleStreams(t *testing.T) {
	conn1, conn2 := mockPipe()
	defer conn1.Close()
	defer conn2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientSession := NewClientSession(ctx, conn1)
	serverSession := NewServerSession(ctx, conn2)

	defer clientSession.Close()
	defer serverSession.Close()

	numStreams := 5

	// Open multiple streams from client
	clientStreams := make([]*VirtualConn, numStreams)
	for i := 0; i < numStreams; i++ {
		stream, err := clientSession.OpenStream()
		if err != nil {
			t.Fatalf("Failed to open client stream %d: %v", i, err)
		}
		clientStreams[i] = stream
		defer stream.Close()
	}

	// Accept multiple streams on server
	serverStreams := make([]*VirtualConn, numStreams)
	for i := 0; i < numStreams; i++ {
		stream, err := serverSession.AcceptStream()
		if err != nil {
			t.Fatalf("Failed to accept server stream %d: %v", i, err)
		}
		serverStreams[i] = stream
		defer stream.Close()
	}

	// Test data exchange on all streams
	for i := 0; i < numStreams; i++ {
		testData := []byte(fmt.Sprintf("Stream %d data", i))

		// Client writes
		n, err := clientStreams[i].Write(testData)
		if err != nil {
			t.Fatalf("Client stream %d write failed: %v", i, err)
		}
		if n != len(testData) {
			t.Fatalf("Client stream %d write incomplete", i)
		}

		// Server reads
		readData := make([]byte, len(testData))
		n, err = io.ReadFull(serverStreams[i], readData)
		if err != nil {
			t.Fatalf("Server stream %d read failed: %v", i, err)
		}

		// Verify
		if string(readData) != string(testData) {
			t.Fatalf("Stream %d data mismatch: got %q, expected %q", i, string(readData), string(testData))
		}
	}
}

func TestStreamClose(t *testing.T) {
	conn1, conn2 := mockPipe()
	defer conn1.Close()
	defer conn2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientSession := NewClientSession(ctx, conn1)
	serverSession := NewServerSession(ctx, conn2)

	defer clientSession.Close()
	defer serverSession.Close()

	// Open and accept stream
	clientStream, err := clientSession.OpenStream()
	if err != nil {
		t.Fatalf("Failed to open client stream: %v", err)
	}

	serverStream, err := serverSession.AcceptStream()
	if err != nil {
		t.Fatalf("Failed to accept server stream: %v", err)
	}
	defer serverStream.Close()

	// Write some data
	testData := []byte("test data")
	n, err := clientStream.Write(testData)
	if err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}
	if n != len(testData) {
		t.Fatalf("Write incomplete: wrote %d, expected %d", n, len(testData))
	}

	// Give a small delay to ensure data is processed
	time.Sleep(10 * time.Millisecond)

	// Close client stream
	err = clientStream.Close()
	if err != nil {
		t.Fatalf("Failed to close client stream: %v", err)
	}

	// Server should read the data first
	readData := make([]byte, len(testData)*2) // Make buffer larger
	n, err = serverStream.Read(readData)
	if err != nil && err != io.EOF {
		t.Fatalf("Server read failed: %v", err)
	}
	if n != len(testData) {
		t.Fatalf("Server read wrong amount: read %d, expected %d", n, len(testData))
	}
	if string(readData[:n]) != string(testData) {
		t.Fatalf("Data mismatch before close: got %q, expected %q", string(readData[:n]), string(testData))
	}

	// Next read should return EOF
	buf := make([]byte, 10)
	n, err = serverStream.Read(buf)
	if err != io.EOF {
		t.Fatalf("Expected EOF after stream close, got %v", err)
	}
	if n != 0 {
		t.Fatalf("Expected 0 bytes after EOF, got %d", n)
	}
}
