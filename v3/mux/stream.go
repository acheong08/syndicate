package mux

import (
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// stream implements the Stream interface
type stream struct {
	id       uint32
	session  *session
	priority uint8

	// State
	localClosed  int32 // atomic
	remoteClosed int32 // atomic
	closeOnce    sync.Once
	closeChan    chan struct{}

	// Data buffers
	readBuffer chan []byte
	readClosed bool
	readMu     sync.Mutex

	// Flow control
	sendWindow int64 // atomic
	recvWindow int64 // atomic

	// Error state
	resetSent   bool
	resetReason ErrorCode
	resetMu     sync.Mutex
}

// newStream creates a new stream
func newStream(id uint32, session *session, initialWindow uint32) *stream {
	s := &stream{
		id:         id,
		session:    session,
		priority:   PriorityNormal,
		readBuffer: make(chan []byte, 32),
		closeChan:  make(chan struct{}),
	}

	atomic.StoreInt64(&s.sendWindow, int64(initialWindow))
	atomic.StoreInt64(&s.recvWindow, int64(initialWindow))

	return s
}

// StreamID returns the unique stream identifier
func (s *stream) StreamID() uint32 {
	return s.id
}

// Priority returns the stream priority
func (s *stream) Priority() uint8 {
	return s.priority
}

// SetPriority sets the stream priority
func (s *stream) SetPriority(priority uint8) error {
	if priority > PriorityHighest {
		return fmt.Errorf("invalid priority: %d", priority)
	}
	s.priority = priority
	// TODO: Send PRIORITY frame
	return nil
}

// Read implements net.Conn.Read
func (s *stream) Read(b []byte) (n int, err error) {
	s.readMu.Lock()
	if s.readClosed {
		s.readMu.Unlock()
		return 0, io.EOF
	}
	s.readMu.Unlock()

	select {
	case data := <-s.readBuffer:
		if data == nil {
			// Stream closed by remote
			s.readMu.Lock()
			s.readClosed = true
			s.readMu.Unlock()
			return 0, io.EOF
		}

		n = copy(b, data)
		if n < len(data) {
			// Put back remaining data
			remaining := make([]byte, len(data)-n)
			copy(remaining, data[n:])
			select {
			case s.readBuffer <- remaining:
			default:
				return n, fmt.Errorf("read buffer overflow")
			}
		}

		// Update receive window
		if s.session.config.EnableFlowControl {
			atomic.AddInt64(&s.recvWindow, -int64(n))
			// Send window update if needed
			if atomic.LoadInt64(&s.recvWindow) < int64(s.session.config.InitialWindowSize)/2 {
				increment := s.session.config.InitialWindowSize
				frame := NewWindowUpdateFrame(s.id, increment)
				s.session.writeFrame(frame)
				atomic.AddInt64(&s.recvWindow, int64(increment))
			}
		}

		return n, nil

	case <-s.closeChan:
		return 0, io.EOF
	case <-s.session.closeChan:
		return 0, fmt.Errorf("session closed")
	}
}

// Write implements net.Conn.Write
func (s *stream) Write(b []byte) (n int, err error) {
	if atomic.LoadInt32(&s.localClosed) == 1 {
		return 0, fmt.Errorf("write on closed stream")
	}

	for len(b) > 0 {
		// Check send window for flow control
		if s.session.config.EnableFlowControl {
			sendWindow := atomic.LoadInt64(&s.sendWindow)
			if sendWindow <= 0 {
				// If no write timeout is configured, don't wait - just fail fast
				if s.session.config.WriteTimeout <= 0 {
					return n, fmt.Errorf("write blocked: no send window available")
				}

				// Wait for window update or timeout
				select {
				case <-time.After(s.session.config.WriteTimeout):
					return n, fmt.Errorf("write timeout: no send window")
				case <-s.closeChan:
					return n, fmt.Errorf("stream closed")
				case <-s.session.closeChan:
					return n, fmt.Errorf("session closed")
				}
			}
		}

		// Determine chunk size
		chunkSize := len(b)
		maxChunk := int(s.session.config.MaxFrameSize)
		if chunkSize > maxChunk {
			chunkSize = maxChunk
		}

		// Respect flow control window
		if s.session.config.EnableFlowControl {
			sendWindow := atomic.LoadInt64(&s.sendWindow)
			if int64(chunkSize) > sendWindow {
				chunkSize = int(sendWindow)
			}
		}

		if chunkSize <= 0 {
			// If no write timeout is configured, don't wait - just fail fast
			if s.session.config.WriteTimeout <= 0 {
				return n, fmt.Errorf("write blocked: no send window available")
			}

			// Wait for window update or timeout
			select {
			case <-time.After(s.session.config.WriteTimeout):
				return n, fmt.Errorf("write timeout: no send window")
			case <-s.closeChan:
				return n, fmt.Errorf("stream closed")
			case <-s.session.closeChan:
				return n, fmt.Errorf("session closed")
			}
		}

		// Create and send frame
		chunk := make([]byte, chunkSize)
		copy(chunk, b[:chunkSize])

		frame := NewDataFrame(s.id, chunk, false)
		if err := s.session.writeFrame(frame); err != nil {
			return n, err
		}

		// Update counters
		n += chunkSize
		b = b[chunkSize:]

		// Update send window
		if s.session.config.EnableFlowControl {
			atomic.AddInt64(&s.sendWindow, -int64(chunkSize))
		}
	}

	return n, nil
}

// Close implements net.Conn.Close
func (s *stream) Close() error {
	return s.CloseWrite()
}

// CloseWrite closes the write side of the stream
func (s *stream) CloseWrite() error {
	if !atomic.CompareAndSwapInt32(&s.localClosed, 0, 1) {
		return nil // Already closed
	}

	// Send END_STREAM frame
	frame := NewDataFrame(s.id, nil, true)
	err := s.session.writeFrame(frame)

	// Update statistics when we close locally (even if remote isn't closed yet)
	// This ensures statistics reflect local stream lifecycle
	wasActive := false
	s.session.streamsMu.RLock()
	_, wasActive = s.session.streams[s.id]
	s.session.streamsMu.RUnlock()

	if wasActive {
		s.session.updateStats(func(stats *Statistics) {
			stats.StreamsClosed++
		})
	}

	// If both sides are closed, clean up (but don't double-count in statistics)
	if atomic.LoadInt32(&s.remoteClosed) == 1 {
		s.forceClose()
	}

	return err
}

// CloseRead closes the read side of the stream
func (s *stream) CloseRead() error {
	s.readMu.Lock()
	s.readClosed = true
	s.readMu.Unlock()

	// Send RST_STREAM if needed
	if atomic.LoadInt32(&s.localClosed) == 0 {
		frame := NewRstStreamFrame(s.id, ErrorCancel)
		s.session.writeFrame(frame)
		atomic.StoreInt32(&s.localClosed, 1)
	}

	s.forceClose()
	return nil
}

// IsClosedWrite returns true if write side is closed
func (s *stream) IsClosedWrite() bool {
	return atomic.LoadInt32(&s.localClosed) == 1
}

// IsClosedRead returns true if read side is closed
func (s *stream) IsClosedRead() bool {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	return s.readClosed
}

// forceClose forcefully closes the stream
func (s *stream) forceClose() {
	s.closeOnce.Do(func() {
		// Signal close
		select {
		case <-s.closeChan:
		default:
			close(s.closeChan)
		}

		// Close read buffer
		s.readMu.Lock()
		s.readClosed = true
		// Signal EOF
		select {
		case s.readBuffer <- nil:
		default:
		}
		s.readMu.Unlock()

		// Remove from session
		s.session.removeStream(s.id)
	})
}

// forceCloseWithoutRemoval forcefully closes the stream without removing it from session
// This is used during session cleanup to avoid circular lock dependency
func (s *stream) forceCloseWithoutRemoval() {
	s.closeOnce.Do(func() {
		// Signal close
		select {
		case <-s.closeChan:
		default:
			close(s.closeChan)
		}

		// Close read buffer
		s.readMu.Lock()
		s.readClosed = true
		// Signal EOF
		select {
		case s.readBuffer <- nil:
		default:
		}
		s.readMu.Unlock()

		// Note: Do NOT call s.session.removeStream(s.id) here
		// The session has already cleared its streams map
	})
}

// handleData processes incoming data for this stream
func (s *stream) handleData(data []byte, endStream bool) error {
	if atomic.LoadInt32(&s.remoteClosed) == 1 {
		return fmt.Errorf("data received on closed stream")
	}

	if endStream {
		atomic.StoreInt32(&s.remoteClosed, 1)
	}

	// Buffer the data
	if len(data) > 0 { // Only buffer non-empty data
		select {
		case s.readBuffer <- data:
		case <-s.closeChan:
			return fmt.Errorf("stream closed")
		default:
			return fmt.Errorf("read buffer full")
		}
	}

	// If end of stream, signal EOF
	if endStream {
		select {
		case s.readBuffer <- nil:
		default:
		}

		// If both sides are closed, clean up
		if atomic.LoadInt32(&s.localClosed) == 1 {
			s.forceClose()
		}
	}

	return nil
}

// handleReset processes a reset for this stream
func (s *stream) handleReset(errorCode ErrorCode) {
	s.resetMu.Lock()
	s.resetReason = errorCode
	s.resetMu.Unlock()

	atomic.StoreInt32(&s.localClosed, 1)
	atomic.StoreInt32(&s.remoteClosed, 1)

	s.forceClose()
}

// handleWindowUpdate processes a window update for this stream
func (s *stream) handleWindowUpdate(increment uint32) error {
	if increment == 0 {
		return fmt.Errorf("window update increment cannot be zero")
	}

	newSize := atomic.AddInt64(&s.sendWindow, int64(increment))
	if newSize > int64(s.session.config.MaxWindowSize) {
		return fmt.Errorf("window size exceeds maximum: %d", newSize)
	}

	return nil
}

// LocalAddr implements net.Conn.LocalAddr
func (s *stream) LocalAddr() net.Addr {
	return &StreamAddr{StreamID: s.id, NetworkName: "mux"}
}

// RemoteAddr implements net.Conn.RemoteAddr
func (s *stream) RemoteAddr() net.Addr {
	return s.session.conn.RemoteAddr()
}

// SetDeadline implements net.Conn.SetDeadline
func (s *stream) SetDeadline(t time.Time) error {
	// TODO: Implement proper deadline support
	return nil
}

// SetReadDeadline implements net.Conn.SetReadDeadline
func (s *stream) SetReadDeadline(t time.Time) error {
	// TODO: Implement proper deadline support
	return nil
}

// SetWriteDeadline implements net.Conn.SetWriteDeadline
func (s *stream) SetWriteDeadline(t time.Time) error {
	// TODO: Implement proper deadline support
	return nil
}

// StreamAddr represents a stream address
type StreamAddr struct {
	StreamID    uint32
	NetworkName string
}

// Network implements net.Addr.Network
func (sa *StreamAddr) Network() string {
	return sa.NetworkName
}

// String implements net.Addr.String
func (sa *StreamAddr) String() string {
	return fmt.Sprintf("mux:%s", string(rune(sa.StreamID)))
}
