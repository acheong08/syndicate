package mux

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// VirtualConn implements net.Conn and represents a multiplexed stream
type VirtualConn struct {
	streamID    uint32
	session     *Session
	readBuffer  chan []byte
	readClosed  bool
	writeClosed bool
	closeOnce   sync.Once
	closed      chan struct{}
	mu          sync.Mutex

	// Flow control
	readWindow  int32
	writeWindow int32
}

// newVirtualConn creates a new virtual connection
func newVirtualConn(streamID uint32, session *Session) *VirtualConn {
	return &VirtualConn{
		streamID:    streamID,
		session:     session,
		readBuffer:  make(chan []byte, 16), // Buffer for incoming data
		closed:      make(chan struct{}),
		readWindow:  65536, // 64KB initial window
		writeWindow: 65536, // 64KB initial window
	}
}

// Read implements net.Conn.Read
func (vc *VirtualConn) Read(b []byte) (n int, err error) {
	vc.mu.Lock()
	if vc.readClosed {
		vc.mu.Unlock()
		return 0, io.EOF
	}
	vc.mu.Unlock()

	select {
	case data := <-vc.readBuffer:
		if data == nil {
			// Stream closed
			vc.mu.Lock()
			vc.readClosed = true
			vc.mu.Unlock()
			return 0, io.EOF
		}
		n = copy(b, data)
		if n < len(data) {
			// Put back remaining data
			remaining := make([]byte, len(data)-n)
			copy(remaining, data[n:])
			select {
			case vc.readBuffer <- remaining:
			default:
				// Buffer full, this shouldn't happen with proper flow control
				return n, fmt.Errorf("read buffer overflow")
			}
		}
		return n, nil
	case <-vc.closed:
		return 0, io.EOF
	case <-vc.session.ctx.Done():
		return 0, vc.session.ctx.Err()
	}
}

// Write implements net.Conn.Write
func (vc *VirtualConn) Write(b []byte) (n int, err error) {
	vc.mu.Lock()
	if vc.writeClosed {
		vc.mu.Unlock()
		return 0, fmt.Errorf("write on closed connection")
	}
	vc.mu.Unlock()

	select {
	case <-vc.closed:
		return 0, fmt.Errorf("write on closed connection")
	case <-vc.session.ctx.Done():
		return 0, vc.session.ctx.Err()
	default:
	}

	// Send data in chunks if necessary
	for len(b) > 0 {
		chunkSize := len(b)
		if chunkSize > 16384 { // 16KB chunks
			chunkSize = 16384
		}

		chunk := make([]byte, chunkSize)
		copy(chunk, b[:chunkSize])

		frame := &Frame{
			StreamID: vc.streamID,
			Type:     FrameTypeData,
			Flags:    0,
			Data:     chunk,
		}

		if err := vc.session.writeFrame(frame); err != nil {
			return n, err
		}

		n += chunkSize
		b = b[chunkSize:]
	}

	return n, nil
}

// Close implements net.Conn.Close
func (vc *VirtualConn) Close() error {
	var err error
	vc.closeOnce.Do(func() {
		// Send close frame
		frame := &Frame{
			StreamID: vc.streamID,
			Type:     FrameTypeStreamClose,
			Flags:    0,
			Data:     nil,
		}

		err = vc.session.writeFrame(frame)

		// Mark as closed
		vc.mu.Lock()
		vc.readClosed = true
		vc.writeClosed = true
		vc.mu.Unlock()

		// Close the closed channel
		close(vc.closed)

		// Remove from session
		vc.session.removeStream(vc.streamID)
	})
	return err
}

// LocalAddr implements net.Conn.LocalAddr
func (vc *VirtualConn) LocalAddr() net.Addr {
	return &VirtualAddr{StreamID: vc.streamID, NetworkName: "mux"}
}

// RemoteAddr implements net.Conn.RemoteAddr
func (vc *VirtualConn) RemoteAddr() net.Addr {
	return vc.session.conn.RemoteAddr()
}

// SetDeadline implements net.Conn.SetDeadline
func (vc *VirtualConn) SetDeadline(t time.Time) error {
	return nil
}

// SetReadDeadline implements net.Conn.SetReadDeadline
func (vc *VirtualConn) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline implements net.Conn.SetWriteDeadline
func (vc *VirtualConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// handleData processes incoming data for this stream
func (vc *VirtualConn) handleData(data []byte) error {
	select {
	case vc.readBuffer <- data:
		return nil
	case <-vc.closed:
		return fmt.Errorf("stream closed")
	case <-vc.session.ctx.Done():
		return vc.session.ctx.Err()
	default:
		// Buffer full, drop the data (in a full implementation, we'd handle backpressure)
		return fmt.Errorf("read buffer full")
	}
}

// handleClose processes a close frame for this stream
func (vc *VirtualConn) handleClose() {
	// Don't set readClosed immediately - let the nil in the buffer handle it
	// Signal end of stream
	select {
	case vc.readBuffer <- nil:
	default:
	}
}

// VirtualAddr represents a virtual connection address
type VirtualAddr struct {
	StreamID    uint32
	NetworkName string
}

// Network implements net.Addr.Network
func (va *VirtualAddr) Network() string {
	return va.NetworkName
}

// String implements net.Addr.String
func (va *VirtualAddr) String() string {
	return fmt.Sprintf("mux:%d", va.StreamID)
}
