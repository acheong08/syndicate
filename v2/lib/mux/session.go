package mux

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
)

// Session manages multiplexed streams over a single connection
type Session struct {
	conn         net.Conn
	ctx          context.Context
	cancel       context.CancelFunc
	isClient     bool
	nextStreamID uint32
	streams      map[uint32]*VirtualConn
	streamsMu    sync.RWMutex
	writeMu      sync.Mutex
	acceptChan   chan *VirtualConn
	closed       chan struct{}
	closeOnce    sync.Once
}

// NewClientSession creates a new client session
func NewClientSession(ctx context.Context, conn net.Conn) *Session {
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{
		conn:         conn,
		ctx:          ctx,
		cancel:       cancel,
		isClient:     true,
		nextStreamID: 1, // Client uses odd stream IDs
		streams:      make(map[uint32]*VirtualConn),
		acceptChan:   make(chan *VirtualConn, 16),
		closed:       make(chan struct{}),
	}

	go s.readLoop()
	return s
}

// NewServerSession creates a new server session
func NewServerSession(ctx context.Context, conn net.Conn) *Session {
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{
		conn:         conn,
		ctx:          ctx,
		cancel:       cancel,
		isClient:     false,
		nextStreamID: 2, // Server uses even stream IDs
		streams:      make(map[uint32]*VirtualConn),
		acceptChan:   make(chan *VirtualConn, 16),
		closed:       make(chan struct{}),
	}

	go s.readLoop()
	return s
}

// OpenStream creates a new outgoing stream
func (s *Session) OpenStream() (*VirtualConn, error) {
	select {
	case <-s.closed:
		return nil, fmt.Errorf("session closed")
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	default:
	}

	streamID := s.nextStreamID
	if s.isClient {
		s.nextStreamID += 2 // Client uses odd IDs: 1, 3, 5...
	} else {
		s.nextStreamID += 2 // Server uses even IDs: 2, 4, 6...
	}

	vc := newVirtualConn(streamID, s)

	s.streamsMu.Lock()
	s.streams[streamID] = vc
	s.streamsMu.Unlock()

	// Send stream open frame
	frame := &Frame{
		StreamID: streamID,
		Type:     FrameTypeStreamOpen,
		Flags:    0,
		Data:     nil,
	}

	if err := s.writeFrame(frame); err != nil {
		s.removeStream(streamID)
		return nil, err
	}

	return vc, nil
}

// AcceptStream accepts an incoming stream
func (s *Session) AcceptStream() (*VirtualConn, error) {
	select {
	case vc := <-s.acceptChan:
		return vc, nil
	case <-s.closed:
		return nil, fmt.Errorf("session closed")
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// Close closes the session
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		// Cancel context
		s.cancel()

		// Close all streams
		s.streamsMu.Lock()
		for _, vc := range s.streams {
			vc.Close()
		}
		s.streamsMu.Unlock()

		// Close underlying connection
		err = s.conn.Close()

		// Close the closed channel
		close(s.closed)
	})
	return err
}

// writeFrame writes a frame to the underlying connection
func (s *Session) writeFrame(frame *Frame) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	select {
	case <-s.closed:
		return fmt.Errorf("session closed")
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
	}

	return WriteFrame(s.conn, frame)
}

// removeStream removes a stream from the session
func (s *Session) removeStream(streamID uint32) {
	s.streamsMu.Lock()
	delete(s.streams, streamID)
	s.streamsMu.Unlock()
}

// readLoop continuously reads frames from the connection
func (s *Session) readLoop() {
	defer s.Close()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		frame, err := ReadFrame(s.conn)
		if err != nil {
			if err != io.EOF {
				// Log error in a real implementation
			}
			return
		}

		if err := s.handleFrame(frame); err != nil {
			// Log error in a real implementation
			continue
		}
	}
}

// handleFrame processes an incoming frame
func (s *Session) handleFrame(frame *Frame) error {
	switch frame.Type {
	case FrameTypeData:
		return s.handleDataFrame(frame)
	case FrameTypeStreamOpen:
		return s.handleStreamOpenFrame(frame)
	case FrameTypeStreamClose:
		return s.handleStreamCloseFrame(frame)
	case FrameTypeWindowUpdate:
		return s.handleWindowUpdateFrame(frame)
	case FrameTypePing:
		return s.handlePingFrame(frame)
	case FrameTypeSettings:
		return s.handleSettingsFrame(frame)
	default:
		// Unknown frame type, ignore
		return nil
	}
}

// handleDataFrame processes a data frame
func (s *Session) handleDataFrame(frame *Frame) error {
	s.streamsMu.RLock()
	vc, exists := s.streams[frame.StreamID]
	s.streamsMu.RUnlock()

	if !exists {
		// Stream doesn't exist, ignore
		return nil
	}

	return vc.handleData(frame.Data)
}

// handleStreamOpenFrame processes a stream open frame
func (s *Session) handleStreamOpenFrame(frame *Frame) error {
	// Check if we should accept this stream
	streamID := frame.StreamID

	// Validate stream ID (client uses odd, server uses even)
	if s.isClient && streamID%2 == 1 {
		// Client receiving odd stream ID, invalid
		return fmt.Errorf("invalid stream ID from server: %d", streamID)
	}
	if !s.isClient && streamID%2 == 0 {
		// Server receiving even stream ID, invalid
		return fmt.Errorf("invalid stream ID from client: %d", streamID)
	}

	vc := newVirtualConn(streamID, s)

	s.streamsMu.Lock()
	s.streams[streamID] = vc
	s.streamsMu.Unlock()

	// Send to accept channel
	select {
	case s.acceptChan <- vc:
		return nil
	case <-s.ctx.Done():
		s.removeStream(streamID)
		return s.ctx.Err()
	default:
		// Accept channel full, drop the stream
		s.removeStream(streamID)
		return fmt.Errorf("accept channel full")
	}
}

// handleStreamCloseFrame processes a stream close frame
func (s *Session) handleStreamCloseFrame(frame *Frame) error {
	s.streamsMu.RLock()
	vc, exists := s.streams[frame.StreamID]
	s.streamsMu.RUnlock()

	if !exists {
		// Stream doesn't exist, ignore
		return nil
	}

	vc.handleClose()
	s.removeStream(frame.StreamID)
	return nil
}

// handleWindowUpdateFrame processes a window update frame
func (s *Session) handleWindowUpdateFrame(frame *Frame) error {
	// TODO: Implement flow control
	return nil
}

// handlePingFrame processes a ping frame
func (s *Session) handlePingFrame(frame *Frame) error {
	// Send ping response
	response := &Frame{
		StreamID: 0,
		Type:     FrameTypePing,
		Flags:    FlagAck,
		Data:     frame.Data,
	}
	return s.writeFrame(response)
}

// handleSettingsFrame processes a settings frame
func (s *Session) handleSettingsFrame(frame *Frame) error {
	// TODO: Implement settings
	return nil
}
