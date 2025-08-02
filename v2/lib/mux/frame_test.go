package mux

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestFrameReadWrite(t *testing.T) {
	tests := []struct {
		name  string
		frame *Frame
	}{
		{
			name: "data frame",
			frame: &Frame{
				StreamID: 123,
				Type:     FrameTypeData,
				Flags:    0,
				Data:     []byte("hello world"),
			},
		},
		{
			name: "stream open frame",
			frame: &Frame{
				StreamID: 456,
				Type:     FrameTypeStreamOpen,
				Flags:    0,
				Data:     nil,
			},
		},
		{
			name: "stream close frame",
			frame: &Frame{
				StreamID: 789,
				Type:     FrameTypeStreamClose,
				Flags:    FlagEndStream,
				Data:     nil,
			},
		},
		{
			name: "ping frame with data",
			frame: &Frame{
				StreamID: 0,
				Type:     FrameTypePing,
				Flags:    FlagAck,
				Data:     []byte("ping data"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			// Write frame
			err := WriteFrame(&buf, tt.frame)
			if err != nil {
				t.Fatalf("WriteFrame failed: %v", err)
			}

			// Read frame back
			readFrame, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame failed: %v", err)
			}

			// Compare frames
			if readFrame.StreamID != tt.frame.StreamID {
				t.Errorf("StreamID mismatch: got %d, want %d", readFrame.StreamID, tt.frame.StreamID)
			}
			if readFrame.Type != tt.frame.Type {
				t.Errorf("Type mismatch: got %v, want %v", readFrame.Type, tt.frame.Type)
			}
			if readFrame.Flags != tt.frame.Flags {
				t.Errorf("Flags mismatch: got %v, want %v", readFrame.Flags, tt.frame.Flags)
			}
			if !bytes.Equal(readFrame.Data, tt.frame.Data) {
				t.Errorf("Data mismatch: got %v, want %v", readFrame.Data, tt.frame.Data)
			}
		})
	}
}

func TestFrameMaxSize(t *testing.T) {
	// Test maximum frame size
	largeData := make([]byte, maxFrameLen)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	frame := &Frame{
		StreamID: 1,
		Type:     FrameTypeData,
		Flags:    0,
		Data:     largeData,
	}

	var buf bytes.Buffer
	err := WriteFrame(&buf, frame)
	if err != nil {
		t.Fatalf("WriteFrame failed for max size frame: %v", err)
	}

	readFrame, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame failed for max size frame: %v", err)
	}

	if !bytes.Equal(readFrame.Data, largeData) {
		t.Error("Large frame data mismatch")
	}
}

func TestFrameTooLarge(t *testing.T) {
	// Test frame that's too large
	tooLargeData := make([]byte, maxFrameLen+1)

	frame := &Frame{
		StreamID: 1,
		Type:     FrameTypeData,
		Flags:    0,
		Data:     tooLargeData,
	}

	var buf bytes.Buffer
	err := WriteFrame(&buf, frame)
	if err == nil {
		t.Fatal("WriteFrame should have failed for oversized frame")
	}
}

// Mock connection for testing
type mockConn struct {
	*bytes.Buffer
	localAddr  net.Addr
	remoteAddr net.Addr
	closed     bool
}

func newMockConn() *mockConn {
	return &mockConn{
		Buffer:     &bytes.Buffer{},
		localAddr:  &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234},
		remoteAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5678},
	}
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr  { return m.localAddr }
func (m *mockConn) RemoteAddr() net.Addr { return m.remoteAddr }

func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// Pipe creates two connected mock connections
func mockPipe() (net.Conn, net.Conn) {
	conn1, conn2 := net.Pipe()
	return conn1, conn2
}
