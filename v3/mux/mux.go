// Package mux provides connection multiplexing capabilities for the v3 transport layer.
// It implements a clean, HTTP/2-inspired multiplexing protocol with proper flow control,
// error handling, and resource management.
package mux

import (
	"context"
	"net"
	"time"
)

// Multiplexer manages multiple streams over a single connection
type Multiplexer interface {
	// OpenStream creates a new outgoing stream
	OpenStream(ctx context.Context) (Stream, error)

	// AcceptStream accepts an incoming stream
	AcceptStream(ctx context.Context) (Stream, error)

	// Close closes the multiplexer and all streams
	Close() error

	// IsClosed returns true if the multiplexer is closed
	IsClosed() bool

	// Statistics returns connection statistics
	Statistics() Statistics
}

// Stream represents a multiplexed stream that implements net.Conn
type Stream interface {
	net.Conn

	// StreamID returns the unique stream identifier
	StreamID() uint32

	// Priority returns the stream priority (0-7, 7 is highest)
	Priority() uint8

	// SetPriority sets the stream priority
	SetPriority(priority uint8) error

	// CloseWrite closes the write side of the stream
	CloseWrite() error

	// CloseRead closes the read side of the stream
	CloseRead() error

	// IsClosedWrite returns true if write side is closed
	IsClosedWrite() bool

	// IsClosedRead returns true if read side is closed
	IsClosedRead() bool
}

// Statistics contains multiplexer statistics
type Statistics struct {
	// Connection statistics
	BytesSent      uint64
	BytesReceived  uint64
	FramesSent     uint64
	FramesReceived uint64

	// Stream statistics
	ActiveStreams uint32
	TotalStreams  uint64
	StreamsOpened uint64
	StreamsClosed uint64

	// Timing statistics
	RTT            time.Duration
	LastActivity   time.Time
	ConnectionTime time.Duration

	// Error statistics
	ProtocolErrors    uint64
	FlowControlErrors uint64
	StreamErrors      uint64
}

// Config contains multiplexer configuration
type Config struct {
	// Flow control settings
	InitialWindowSize    uint32 // Initial per-stream window size (default: 64KB)
	MaxWindowSize        uint32 // Maximum per-stream window size (default: 16MB)
	ConnectionWindowSize uint32 // Connection-level window size (default: 1MB)

	// Stream settings
	MaxConcurrentStreams uint32 // Maximum concurrent streams (default: 1000)
	MaxFrameSize         uint32 // Maximum frame size (default: 16KB)

	// Timing settings
	KeepAliveInterval time.Duration // Keep-alive ping interval (default: 30s)
	IdleTimeout       time.Duration // Idle connection timeout (default: 5m)
	ReadTimeout       time.Duration // Read operation timeout (default: 30s)
	WriteTimeout      time.Duration // Write operation timeout (default: 30s)

	// Behavior settings
	EnableFlowControl bool // Enable flow control (default: true)
	EnableCompression bool // Enable header compression (default: false)
	DisableUpgrade    bool // Disable protocol upgrade (default: false)
}

// DefaultConfig returns a default configuration optimized for HTTP workloads
func DefaultConfig() *Config {
	return &Config{
		InitialWindowSize:    65536,    // 64KB
		MaxWindowSize:        16777216, // 16MB
		ConnectionWindowSize: 1048576,  // 1MB
		MaxConcurrentStreams: 1000,
		MaxFrameSize:         16384, // 16KB
		KeepAliveInterval:    30 * time.Second,
		IdleTimeout:          5 * time.Minute, // Back to 5 minutes
		ReadTimeout:          0,               // Disable read timeout - let application handle this
		WriteTimeout:         0,               // Disable write timeout - let application handle this
		EnableFlowControl:    false,           // Disabled for HTTP workloads to avoid flow control complexity
		EnableCompression:    false,
		DisableUpgrade:       false,
	}
}

// NewClient creates a client-side multiplexer
func NewClient(conn net.Conn, config *Config) Multiplexer {
	if config == nil {
		config = DefaultConfig()
	}
	return newSession(conn, true, config)
}

// NewServer creates a server-side multiplexer
func NewServer(conn net.Conn, config *Config) Multiplexer {
	if config == nil {
		config = DefaultConfig()
	}
	return newSession(conn, false, config)
}

// Error types
type Error struct {
	Code     ErrorCode
	Message  string
	StreamID uint32
}

func (e *Error) Error() string {
	if e.StreamID == 0 {
		return e.Message
	}
	return e.Message + " (stream " + string(rune(e.StreamID)) + ")"
}

type ErrorCode uint32

const (
	ErrorNoError            ErrorCode = 0x0
	ErrorProtocolError      ErrorCode = 0x1
	ErrorInternalError      ErrorCode = 0x2
	ErrorFlowControlError   ErrorCode = 0x3
	ErrorSettingsTimeout    ErrorCode = 0x4
	ErrorStreamClosed       ErrorCode = 0x5
	ErrorFrameSizeError     ErrorCode = 0x6
	ErrorRefusedStream      ErrorCode = 0x7
	ErrorCancel             ErrorCode = 0x8
	ErrorCompressionError   ErrorCode = 0x9
	ErrorConnectError       ErrorCode = 0xa
	ErrorEnhanceYourCalm    ErrorCode = 0xb
	ErrorInadequateSecurity ErrorCode = 0xc
	ErrorHTTP11Required     ErrorCode = 0xd
)

// Priority levels for streams
const (
	PriorityLowest  uint8 = 0
	PriorityLow     uint8 = 2
	PriorityNormal  uint8 = 4
	PriorityHigh    uint8 = 6
	PriorityHighest uint8 = 7
)
