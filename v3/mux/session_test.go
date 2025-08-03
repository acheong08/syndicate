package mux

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// mockConn implements net.Conn for testing
type mockConn struct {
	readBuffer  *bytes.Buffer
	writeBuffer *bytes.Buffer
	closed      bool
	mu          sync.Mutex
	readCond    *sync.Cond
	peerConn    *mockConn // Reference to the paired connection
}

func newMockConnPair() (*mockConn, *mockConn) {
	buf1 := &bytes.Buffer{}
	buf2 := &bytes.Buffer{}

	conn1 := &mockConn{
		readBuffer:  buf2,
		writeBuffer: buf1,
	}
	conn1.readCond = sync.NewCond(&conn1.mu)

	conn2 := &mockConn{
		readBuffer:  buf1,
		writeBuffer: buf2,
	}
	conn2.readCond = sync.NewCond(&conn2.mu)

	// Set up peer references
	conn1.peerConn = conn2
	conn2.peerConn = conn1

	return conn1, conn2
}

func (mc *mockConn) Read(b []byte) (n int, err error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for {
		if mc.closed {
			return 0, io.EOF
		}

		n, err = mc.readBuffer.Read(b)
		if n > 0 || err != io.EOF {
			return n, err
		}

		// No data available, wait for data to be written
		mc.readCond.Wait()
	}
}

func (mc *mockConn) Write(b []byte) (n int, err error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.closed {
		return 0, io.ErrClosedPipe
	}

	n, err = mc.writeBuffer.Write(b)
	if n > 0 && mc.peerConn != nil {
		// Signal the peer that data is available
		mc.peerConn.mu.Lock()
		mc.peerConn.readCond.Broadcast()
		mc.peerConn.mu.Unlock()
	}
	return n, err
}
func (mc *mockConn) Close() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.closed {
		return nil
	}

	mc.closed = true
	mc.readCond.Broadcast() // Wake up any blocked readers
	return nil
}

func (mc *mockConn) LocalAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234} }
func (mc *mockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5678}
}
func (mc *mockConn) SetDeadline(t time.Time) error      { return nil }
func (mc *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (mc *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func TestSessionBasic(t *testing.T) {
	clientConn, serverConn := newMockConnPair()

	config := DefaultConfig()
	config.KeepAliveInterval = 0 // Disable keep-alive for testing

	client := newSession(clientConn, true, config)
	server := newSession(serverConn, false, config)

	defer client.Close()
	defer server.Close()

	// Test basic properties
	if client.IsClosed() {
		t.Error("Client should not be closed initially")
	}
	if server.IsClosed() {
		t.Error("Server should not be closed initially")
	}

	// Test statistics
	stats := client.Statistics()
	if stats.ActiveStreams != 0 {
		t.Errorf("Active streams = %d, want 0", stats.ActiveStreams)
	}
}

func TestStreamOpenAndAccept(t *testing.T) {
	clientConn, serverConn := newMockConnPair()

	config := DefaultConfig()
	config.KeepAliveInterval = 0

	client := newSession(clientConn, true, config)
	server := newSession(serverConn, false, config)

	defer client.Close()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Client opens a stream
	clientStream, err := client.OpenStream(ctx)
	if err != nil {
		t.Fatalf("Failed to open client stream: %v", err)
	}

	// Server accepts the stream
	serverStream, err := server.AcceptStream(ctx)
	if err != nil {
		t.Fatalf("Failed to accept server stream: %v", err)
	}

	// Verify stream IDs
	if clientStream.StreamID() == 0 {
		t.Error("Client stream ID should not be 0")
	}
	if serverStream.StreamID() != clientStream.StreamID() {
		t.Errorf("Stream IDs don't match: client=%d, server=%d",
			clientStream.StreamID(), serverStream.StreamID())
	}

	// Test stream properties
	if clientStream.Priority() != PriorityNormal {
		t.Errorf("Client stream priority = %d, want %d", clientStream.Priority(), PriorityNormal)
	}

	clientStream.Close()
	serverStream.Close()
}

func TestStreamDataTransfer(t *testing.T) {
	clientConn, serverConn := newMockConnPair()

	config := DefaultConfig()
	config.KeepAliveInterval = 0
	config.EnableFlowControl = false // Disable for simplicity

	client := newSession(clientConn, true, config)
	server := newSession(serverConn, false, config)

	defer client.Close()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Open stream
	clientStream, err := client.OpenStream(ctx)
	if err != nil {
		t.Fatalf("Failed to open client stream: %v", err)
	}

	serverStream, err := server.AcceptStream(ctx)
	if err != nil {
		t.Fatalf("Failed to accept server stream: %v", err)
	}

	// Test data transfer
	testData := []byte("Hello, multiplexed world!")

	// Write from client to server
	go func() {
		n, err := clientStream.Write(testData)
		if err != nil {
			t.Errorf("Client write error: %v", err)
		}
		if n != len(testData) {
			t.Errorf("Client wrote %d bytes, want %d", n, len(testData))
		}
		clientStream.CloseWrite()
	}()

	// Read on server side
	readData := make([]byte, len(testData)*2) // Larger buffer
	n, err := serverStream.Read(readData)
	if err != nil && err != io.EOF {
		t.Fatalf("Server read error: %v", err)
	}

	if n != len(testData) {
		t.Errorf("Server read %d bytes, want %d", n, len(testData))
	}

	if !bytes.Equal(readData[:n], testData) {
		t.Errorf("Data mismatch: got %q, want %q", readData[:n], testData)
	}

	clientStream.Close()
	serverStream.Close()
}

func TestStreamPriority(t *testing.T) {
	clientConn, serverConn := newMockConnPair()

	config := DefaultConfig()
	config.KeepAliveInterval = 0

	client := newSession(clientConn, true, config)
	server := newSession(serverConn, false, config)

	defer client.Close()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.OpenStream(ctx)
	if err != nil {
		t.Fatalf("Failed to open stream: %v", err)
	}
	defer stream.Close()

	// Test priority setting
	err = stream.SetPriority(PriorityHigh)
	if err != nil {
		t.Errorf("Failed to set priority: %v", err)
	}

	if stream.Priority() != PriorityHigh {
		t.Errorf("Priority = %d, want %d", stream.Priority(), PriorityHigh)
	}

	// Test invalid priority
	err = stream.SetPriority(255)
	if err == nil {
		t.Error("Should have failed to set invalid priority")
	}
}

func TestSessionClose(t *testing.T) {
	clientConn, serverConn := newMockConnPair()

	config := DefaultConfig()
	config.KeepAliveInterval = 0

	client := newSession(clientConn, true, config)
	server := newSession(serverConn, false, config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Open some streams
	stream1, err := client.OpenStream(ctx)
	if err != nil {
		t.Fatalf("Failed to open stream 1: %v", err)
	}

	stream2, err := client.OpenStream(ctx)
	if err != nil {
		t.Fatalf("Failed to open stream 2: %v", err)
	}

	// Close client session
	client.Close()

	// Verify session is closed
	if !client.IsClosed() {
		t.Error("Client should be closed")
	}

	// Verify streams are closed
	_, err = stream1.Write([]byte("test"))
	if err == nil {
		t.Error("Write to closed stream should fail")
	}

	_, err = stream2.Write([]byte("test"))
	if err == nil {
		t.Error("Write to closed stream should fail")
	}

	// Verify new stream creation fails
	_, err = client.OpenStream(ctx)
	if err == nil {
		t.Error("Opening stream on closed session should fail")
	}

	server.Close()
}

func TestSessionStatistics(t *testing.T) {
	clientConn, serverConn := newMockConnPair()

	config := DefaultConfig()
	config.KeepAliveInterval = 0
	config.EnableFlowControl = false

	client := newSession(clientConn, true, config)
	server := newSession(serverConn, false, config)

	defer client.Close()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Initial statistics
	stats := client.Statistics()
	if stats.StreamsOpened != 0 {
		t.Errorf("Initial streams opened = %d, want 0", stats.StreamsOpened)
	}

	// Open a stream
	stream, err := client.OpenStream(ctx)
	if err != nil {
		t.Fatalf("Failed to open stream: %v", err)
	}

	// Check statistics after opening stream
	stats = client.Statistics()
	if stats.StreamsOpened != 1 {
		t.Errorf("Streams opened = %d, want 1", stats.StreamsOpened)
	}
	if stats.ActiveStreams != 1 {
		t.Errorf("Active streams = %d, want 1", stats.ActiveStreams)
	}

	// Close stream
	stream.Close()

	// Give some time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Check statistics after closing stream
	stats = client.Statistics()
	if stats.StreamsClosed != 1 {
		t.Errorf("Streams closed = %d, want 1", stats.StreamsClosed)
	}
}

func TestConfig(t *testing.T) {
	config := DefaultConfig()

	// Test default values
	if config.InitialWindowSize != 65536 {
		t.Errorf("Default InitialWindowSize = %d, want 65536", config.InitialWindowSize)
	}
	if config.MaxConcurrentStreams != 1000 {
		t.Errorf("Default MaxConcurrentStreams = %d, want 1000", config.MaxConcurrentStreams)
	}
	if config.EnableFlowControl != false {
		t.Error("Default EnableFlowControl should be false for HTTP workloads")
	}

	// Test custom config
	customConfig := &Config{
		InitialWindowSize:    32768,
		MaxConcurrentStreams: 500,
		EnableFlowControl:    false,
	}

	clientConn, _ := newMockConnPair()
	session := newSession(clientConn, true, customConfig)
	defer session.Close()

	if session.config.InitialWindowSize != 32768 {
		t.Errorf("Custom InitialWindowSize = %d, want 32768", session.config.InitialWindowSize)
	}
}

func TestStreamAddressing(t *testing.T) {
	clientConn, serverConn := newMockConnPair()

	config := DefaultConfig()
	config.KeepAliveInterval = 0

	client := newSession(clientConn, true, config)
	server := newSession(serverConn, false, config)

	defer client.Close()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.OpenStream(ctx)
	if err != nil {
		t.Fatalf("Failed to open stream: %v", err)
	}
	defer stream.Close()

	// Test local address
	localAddr := stream.LocalAddr()
	if localAddr.Network() != "mux" {
		t.Errorf("Local addr network = %q, want %q", localAddr.Network(), "mux")
	}

	expectedLocal := "mux:" + string(rune(stream.StreamID()))
	if localAddr.String() != expectedLocal {
		t.Errorf("Local addr string = %q, want %q", localAddr.String(), expectedLocal)
	}

	// Test remote address
	remoteAddr := stream.RemoteAddr()
	if remoteAddr == nil {
		t.Error("Remote address should not be nil")
	}
}

func BenchmarkSessionOpenClose(b *testing.B) {
	clientConn, serverConn := newMockConnPair()

	config := DefaultConfig()
	config.KeepAliveInterval = 0
	config.EnableFlowControl = false

	client := newSession(clientConn, true, config)
	server := newSession(serverConn, false, config)

	defer client.Close()
	defer server.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			stream, err := client.OpenStream(ctx)
			if err != nil {
				b.Fatalf("Failed to open stream: %v", err)
			}
			stream.Close()
		}
	})
}

func BenchmarkStreamWrite(b *testing.B) {
	clientConn, serverConn := newMockConnPair()

	config := DefaultConfig()
	config.KeepAliveInterval = 0
	config.EnableFlowControl = false

	client := newSession(clientConn, true, config)
	server := newSession(serverConn, false, config)

	defer client.Close()
	defer server.Close()

	ctx := context.Background()
	stream, err := client.OpenStream(ctx)
	if err != nil {
		b.Fatalf("Failed to open stream: %v", err)
	}
	defer stream.Close()

	data := make([]byte, 1024)

	b.ResetTimer()
	b.SetBytes(1024)

	for i := 0; i < b.N; i++ {
		stream.Write(data)
	}
}
