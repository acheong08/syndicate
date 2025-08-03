package mux

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// session implements the Multiplexer interface
type session struct {
	conn     net.Conn
	config   *Config
	isClient bool

	// Context and cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// Stream management
	nextStreamID uint32
	streams      map[uint32]*stream
	streamsMu    sync.RWMutex
	acceptChan   chan *stream

	// Connection state
	closed    int32 // atomic
	closeOnce sync.Once
	closeChan chan struct{}

	// Frame I/O
	writeMu        sync.Mutex
	writeFrameChan chan *Frame

	// Flow control
	connWindowSize int64 // atomic
	connWindowUsed int64 // atomic

	// Statistics
	stats atomic.Value // Statistics

	// Keep-alive
	lastActivity int64 // atomic (unix timestamp)
	pingData     []byte
	pingSent     int64 // atomic (unix timestamp)
	rtt          int64 // atomic (nanoseconds)

	// Error handling
	lastError  atomic.Value // error
	goAwaySent bool
	goAwayMu   sync.Mutex
}

// newSession creates a new multiplexer session
func newSession(conn net.Conn, isClient bool, config *Config) *session {
	ctx, cancel := context.WithCancel(context.Background())

	s := &session{
		conn:           conn,
		config:         config,
		isClient:       isClient,
		ctx:            ctx,
		cancel:         cancel,
		streams:        make(map[uint32]*stream),
		acceptChan:     make(chan *stream, 64),
		closeChan:      make(chan struct{}),
		writeFrameChan: make(chan *Frame, 256),
		pingData:       make([]byte, 8),
	}

	// Initialize stream ID counter
	if isClient {
		s.nextStreamID = 1 // Client uses odd stream IDs: 1, 3, 5...
	} else {
		s.nextStreamID = 2 // Server uses even stream IDs: 2, 4, 6...
	}

	// Initialize connection window
	atomic.StoreInt64(&s.connWindowSize, int64(config.ConnectionWindowSize))
	atomic.StoreInt64(&s.lastActivity, time.Now().Unix())

	// Initialize statistics
	s.stats.Store(Statistics{
		ConnectionTime: time.Since(time.Now()), // Will be 0
		LastActivity:   time.Now(),
	})

	// Generate random ping data
	copy(s.pingData, "ping-req")

	// Start background goroutines
	go s.writeLoop()
	go s.readLoop()
	go s.keepAliveLoop()

	return s
}

// OpenStream creates a new outgoing stream
func (s *session) OpenStream(ctx context.Context) (Stream, error) {
	if s.IsClosed() {
		return nil, fmt.Errorf("session is closed")
	}

	// Check max concurrent streams
	s.streamsMu.RLock()
	activeCount := uint32(len(s.streams))
	s.streamsMu.RUnlock()

	if activeCount >= s.config.MaxConcurrentStreams {
		return nil, fmt.Errorf("maximum concurrent streams reached: %d", s.config.MaxConcurrentStreams)
	}

	// Get next stream ID
	streamID := atomic.AddUint32(&s.nextStreamID, 2) - 2

	// Create stream
	stream := newStream(streamID, s, s.config.InitialWindowSize)

	// Add to streams map
	s.streamsMu.Lock()
	s.streams[streamID] = stream
	s.streamsMu.Unlock()

	// Don't send initial empty DATA frame - let the application send actual data
	// This optimizes for HTTP request/response patterns where the first data is meaningful

	// Send initial window update if flow control is enabled
	if s.config.EnableFlowControl && s.config.InitialWindowSize > 0 {
		windowFrame := NewWindowUpdateFrame(streamID, s.config.InitialWindowSize)
		if err := s.writeFrame(windowFrame); err != nil {
			s.removeStream(streamID)
			return nil, fmt.Errorf("failed to send initial window update: %w", err)
		}
	} else {
		// If flow control is disabled, send an empty DATA frame to notify the remote peer
		// about the new stream
		emptyFrame := NewDataFrame(streamID, nil, false)
		if err := s.writeFrame(emptyFrame); err != nil {
			s.removeStream(streamID)
			return nil, fmt.Errorf("failed to send initial empty frame: %w", err)
		}
	}

	// Update statistics
	s.updateStats(func(stats *Statistics) {
		stats.ActiveStreams++
		stats.TotalStreams++
		stats.StreamsOpened++
	})

	return stream, nil
}

// AcceptStream accepts an incoming stream
func (s *session) AcceptStream(ctx context.Context) (Stream, error) {
	select {
	case stream := <-s.acceptChan:
		return stream, nil
	case <-s.closeChan:
		return nil, fmt.Errorf("session is closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close closes the session and all streams
func (s *session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		// Mark as closed
		atomic.StoreInt32(&s.closed, 1)

		// Send GOAWAY frame
		s.sendGoAway(ErrorNoError, nil)

		// Close all streams - extract stream list first to avoid holding lock during cleanup
		s.streamsMu.Lock()
		streams := make([]*stream, 0, len(s.streams))
		for _, stream := range s.streams {
			streams = append(streams, stream)
		}
		s.streams = make(map[uint32]*stream)
		s.streamsMu.Unlock()

		// Now close streams without holding the lock
		for _, stream := range streams {
			stream.forceCloseWithoutRemoval()
		}

		// Cancel context
		s.cancel()

		// Close underlying connection
		err = s.conn.Close()

		// Close channels
		close(s.closeChan)
		close(s.writeFrameChan)
		close(s.acceptChan)
	})
	return err
}

// IsClosed returns true if the session is closed
func (s *session) IsClosed() bool {
	return atomic.LoadInt32(&s.closed) == 1
}

// Statistics returns connection statistics
func (s *session) Statistics() Statistics {
	stats := s.stats.Load().(Statistics)

	// Update runtime values
	stats.LastActivity = time.Unix(atomic.LoadInt64(&s.lastActivity), 0)
	stats.RTT = time.Duration(atomic.LoadInt64(&s.rtt))

	s.streamsMu.RLock()
	stats.ActiveStreams = uint32(len(s.streams))
	s.streamsMu.RUnlock()

	return stats
}

// writeFrame sends a frame to the write loop
func (s *session) writeFrame(frame *Frame) error {
	if s.IsClosed() {
		return fmt.Errorf("session is closed")
	}

	select {
	case s.writeFrameChan <- frame:
		return nil
	case <-s.closeChan:
		return fmt.Errorf("session is closed")
	default:
		return fmt.Errorf("write buffer full")
	}
}

// writeLoop handles frame writing to the connection
func (s *session) writeLoop() {
	defer s.Close()

	for {
		select {
		case frame := <-s.writeFrameChan:
			if frame == nil {
				return
			}

			// Validate frame before writing
			if err := frame.Validate(); err != nil {
				s.handleError(fmt.Errorf("invalid frame: %w", err))
				continue
			}

			// Write frame with timeout
			if s.config.WriteTimeout > 0 {
				s.conn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout))
			}

			s.writeMu.Lock()
			err := WriteFrame(s.conn, frame)
			s.writeMu.Unlock()

			if err != nil {
				s.handleError(fmt.Errorf("failed to write frame: %w", err))
				return
			}

			// Update statistics
			s.updateStats(func(stats *Statistics) {
				stats.FramesSent++
				stats.BytesSent += uint64(len(frame.Data)) + frameHeaderLen
				stats.LastActivity = time.Now()
			})

			atomic.StoreInt64(&s.lastActivity, time.Now().Unix())

		case <-s.closeChan:
			return
		}
	}
}

// readLoop handles frame reading from the connection
func (s *session) readLoop() {
	defer s.Close()

	for {
		// Read frame with timeout
		if s.config.ReadTimeout > 0 {
			s.conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout))
		}

		frame, err := ReadFrame(s.conn)
		if err != nil {
			if !s.IsClosed() {
				s.handleError(fmt.Errorf("failed to read frame: %w", err))
			}
			return
		}

		// Validate frame
		if err := frame.Validate(); err != nil {
			s.handleError(fmt.Errorf("invalid frame received: %w", err))
			continue
		}

		// Update statistics
		s.updateStats(func(stats *Statistics) {
			stats.FramesReceived++
			stats.BytesReceived += uint64(len(frame.Data)) + frameHeaderLen
		})

		atomic.StoreInt64(&s.lastActivity, time.Now().Unix())

		// Handle frame
		if err := s.handleFrame(frame); err != nil {
			s.handleError(fmt.Errorf("failed to handle frame: %w", err))
		}
	}
}

// handleFrame processes incoming frames
func (s *session) handleFrame(frame *Frame) error {
	switch frame.Type {
	case FrameTypeData:
		return s.handleDataFrame(frame)
	case FrameTypeRstStream:
		return s.handleRstStreamFrame(frame)
	case FrameTypeSettings:
		return s.handleSettingsFrame(frame)
	case FrameTypePing:
		return s.handlePingFrame(frame)
	case FrameTypeGoAway:
		return s.handleGoAwayFrame(frame)
	case FrameTypeWindowUpdate:
		return s.handleWindowUpdateFrame(frame)
	default:
		// Unknown frame type, ignore
		return nil
	}
}

// handleDataFrame processes a DATA frame
func (s *session) handleDataFrame(frame *Frame) error {
	if frame.StreamID == 0 {
		return fmt.Errorf("DATA frame cannot have stream ID 0")
	}

	s.streamsMu.RLock()
	stream, exists := s.streams[frame.StreamID]
	s.streamsMu.RUnlock()

	if !exists {
		// Check if this is a new stream we should accept
		if s.shouldAcceptNewStream(frame.StreamID) {
			stream = newStream(frame.StreamID, s, s.config.InitialWindowSize)
			s.streamsMu.Lock()
			s.streams[frame.StreamID] = stream
			s.streamsMu.Unlock()

			// Send to accept channel
			select {
			case s.acceptChan <- stream:
			case <-s.closeChan:
				return fmt.Errorf("session closed")
			default:
				// Accept channel full, reject stream
				rstFrame := NewRstStreamFrame(frame.StreamID, ErrorRefusedStream)
				s.writeFrame(rstFrame)
				s.removeStream(frame.StreamID)
				return nil
			}
		} else {
			// Stream doesn't exist, send RST_STREAM
			rstFrame := NewRstStreamFrame(frame.StreamID, ErrorStreamClosed)
			s.writeFrame(rstFrame)
			return nil
		}
	}

	endStream := frame.Flags&FlagEndStream != 0
	return stream.handleData(frame.Data, endStream)
}

// shouldAcceptNewStream determines if we should accept a new stream
func (s *session) shouldAcceptNewStream(streamID uint32) bool {
	// Check stream ID parity (clients use odd, servers use even)
	if s.isClient {
		return streamID%2 == 0 // Client accepts even stream IDs from server
	} else {
		return streamID%2 == 1 // Server accepts odd stream IDs from client
	}
}

// handleRstStreamFrame processes a RST_STREAM frame
func (s *session) handleRstStreamFrame(frame *Frame) error {
	if frame.StreamID == 0 {
		return fmt.Errorf("RST_STREAM frame cannot have stream ID 0")
	}

	if len(frame.Data) != 4 {
		return fmt.Errorf("RST_STREAM frame must have 4-byte payload")
	}

	errorCode := ErrorCode(binary.BigEndian.Uint32(frame.Data))

	s.streamsMu.RLock()
	stream, exists := s.streams[frame.StreamID]
	s.streamsMu.RUnlock()

	if exists {
		stream.handleReset(errorCode)
	}

	return nil
}

// handleSettingsFrame processes a SETTINGS frame
func (s *session) handleSettingsFrame(frame *Frame) error {
	if frame.StreamID != 0 {
		return fmt.Errorf("SETTINGS frame must have stream ID 0")
	}

	if frame.Flags&FlagAck != 0 {
		// This is a SETTINGS ACK, nothing to do
		return nil
	}

	// Parse settings (each setting is 6 bytes: 2-byte ID + 4-byte value)
	data := frame.Data
	for len(data) >= 6 {
		settingID := binary.BigEndian.Uint16(data[0:2])
		settingValue := binary.BigEndian.Uint32(data[2:6])

		switch settingID {
		case 1: // SETTINGS_HEADER_TABLE_SIZE
			// Not implemented yet
		case 2: // SETTINGS_ENABLE_PUSH
			// Not implemented yet
		case 3: // SETTINGS_MAX_CONCURRENT_STREAMS
			if settingValue > 0 {
				s.config.MaxConcurrentStreams = settingValue
			}
		case 4: // SETTINGS_INITIAL_WINDOW_SIZE
			if settingValue <= 0x7fffffff {
				s.config.InitialWindowSize = settingValue
			}
		case 5: // SETTINGS_MAX_FRAME_SIZE
			if settingValue >= 16384 && settingValue <= 16777215 {
				s.config.MaxFrameSize = settingValue
			}
		case 6: // SETTINGS_MAX_HEADER_LIST_SIZE
			// Not implemented yet
		}

		data = data[6:]
	}

	// Send SETTINGS ACK
	ackFrame := &Frame{
		StreamID: 0,
		Type:     FrameTypeSettings,
		Flags:    FlagAck,
		Data:     nil,
	}
	return s.writeFrame(ackFrame)
}

// handlePingFrame processes a PING frame
func (s *session) handlePingFrame(frame *Frame) error {
	if frame.StreamID != 0 {
		return fmt.Errorf("PING frame must have stream ID 0")
	}

	if len(frame.Data) != 8 {
		return fmt.Errorf("PING frame must have 8-byte payload")
	}

	if frame.Flags&FlagAck != 0 {
		// This is a PING ACK, calculate RTT
		if pingSent := atomic.LoadInt64(&s.pingSent); pingSent > 0 {
			rtt := time.Now().UnixNano() - pingSent
			atomic.StoreInt64(&s.rtt, rtt)
			atomic.StoreInt64(&s.pingSent, 0)
		}
		return nil
	}

	// Send PING ACK
	ackFrame := NewPingFrame(frame.Data, true)
	return s.writeFrame(ackFrame)
}

// handleGoAwayFrame processes a GOAWAY frame
func (s *session) handleGoAwayFrame(frame *Frame) error {
	if frame.StreamID != 0 {
		return fmt.Errorf("GOAWAY frame must have stream ID 0")
	}

	if len(frame.Data) < 8 {
		return fmt.Errorf("GOAWAY frame must have at least 8-byte payload")
	}

	lastStreamID := binary.BigEndian.Uint32(frame.Data[0:4]) & 0x7fffffff
	errorCode := ErrorCode(binary.BigEndian.Uint32(frame.Data[4:8]))

	// Close streams with ID > lastStreamID
	s.streamsMu.Lock()
	for streamID, stream := range s.streams {
		if streamID > lastStreamID {
			stream.handleReset(errorCode)
			delete(s.streams, streamID)
		}
	}
	s.streamsMu.Unlock()

	// Close the session
	go s.Close()

	return nil
}

// handleWindowUpdateFrame processes a WINDOW_UPDATE frame
func (s *session) handleWindowUpdateFrame(frame *Frame) error {
	if len(frame.Data) != 4 {
		return fmt.Errorf("WINDOW_UPDATE frame must have 4-byte payload")
	}

	increment := binary.BigEndian.Uint32(frame.Data) & 0x7fffffff
	if increment == 0 {
		return fmt.Errorf("WINDOW_UPDATE increment cannot be zero")
	}

	if frame.StreamID == 0 {
		// Connection-level window update
		newSize := atomic.AddInt64(&s.connWindowSize, int64(increment))
		if newSize > 0x7fffffff {
			return fmt.Errorf("connection window size overflow")
		}
	} else {
		// Stream-level window update
		s.streamsMu.RLock()
		stream, exists := s.streams[frame.StreamID]
		s.streamsMu.RUnlock()

		if exists {
			return stream.handleWindowUpdate(increment)
		}
		// Stream doesn't exist, ignore
	}

	return nil
}

// removeStream removes a stream from the session
func (s *session) removeStream(streamID uint32) {
	s.streamsMu.Lock()
	delete(s.streams, streamID)
	s.streamsMu.Unlock()

	s.updateStats(func(stats *Statistics) {
		stats.ActiveStreams--
		// Note: StreamsClosed is updated in CloseWrite to avoid double-counting
	})
}

// sendGoAway sends a GOAWAY frame
func (s *session) sendGoAway(errorCode ErrorCode, debugData []byte) {
	s.goAwayMu.Lock()
	defer s.goAwayMu.Unlock()

	if s.goAwaySent {
		return
	}
	s.goAwaySent = true

	// Get last stream ID
	s.streamsMu.RLock()
	var lastStreamID uint32
	for streamID := range s.streams {
		if streamID > lastStreamID {
			lastStreamID = streamID
		}
	}
	s.streamsMu.RUnlock()

	frame := NewGoAwayFrame(lastStreamID, errorCode, debugData)
	s.writeFrame(frame)
}

// handleError handles session-level errors
func (s *session) handleError(err error) {
	s.lastError.Store(err)
	s.updateStats(func(stats *Statistics) {
		stats.ProtocolErrors++
	})
}

// updateStats updates session statistics atomically
func (s *session) updateStats(fn func(*Statistics)) {
	for {
		oldStats := s.stats.Load().(Statistics)
		newStats := oldStats
		fn(&newStats)
		if s.stats.CompareAndSwap(oldStats, newStats) {
			break
		}
	}
}

// keepAliveLoop handles keep-alive pings
func (s *session) keepAliveLoop() {
	if s.config.KeepAliveInterval <= 0 {
		return
	}

	ticker := time.NewTicker(s.config.KeepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if connection is idle
			lastActivity := atomic.LoadInt64(&s.lastActivity)
			if time.Since(time.Unix(lastActivity, 0)) >= s.config.IdleTimeout {
				s.sendGoAway(ErrorNoError, []byte("idle timeout"))
				s.Close()
				return
			}

			// Send ping if no recent activity
			if time.Since(time.Unix(lastActivity, 0)) >= s.config.KeepAliveInterval {
				pingFrame := NewPingFrame(s.pingData, false)
				atomic.StoreInt64(&s.pingSent, time.Now().UnixNano())
				if err := s.writeFrame(pingFrame); err != nil {
					return
				}
			}

		case <-s.closeChan:
			return
		}
	}
}
