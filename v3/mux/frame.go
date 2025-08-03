package mux

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Frame represents a multiplexing frame with improved design over v2
type Frame struct {
	StreamID uint32
	Type     FrameType
	Flags    FrameFlags
	Data     []byte
}

// FrameType represents different types of frames with additional types for better flow control
type FrameType uint8

const (
	// Data frame carries application data for a stream
	FrameTypeData FrameType = 0x0
	// Headers frame carries stream headers (for future HTTP/2 compatibility)
	FrameTypeHeaders FrameType = 0x1
	// Priority frame specifies stream priority
	FrameTypePriority FrameType = 0x2
	// RstStream resets a stream
	FrameTypeRstStream FrameType = 0x3
	// Settings contains connection settings
	FrameTypeSettings FrameType = 0x4
	// PushPromise for server push (reserved for future use)
	FrameTypePushPromise FrameType = 0x5
	// Ping for keepalive and RTT measurement
	FrameTypePing FrameType = 0x6
	// GoAway gracefully closes the connection
	FrameTypeGoAway FrameType = 0x7
	// WindowUpdate provides flow control
	FrameTypeWindowUpdate FrameType = 0x8
	// Continuation continues a fragmented headers frame
	FrameTypeContinuation FrameType = 0x9
)

// FrameFlags represents frame flags with better organization
type FrameFlags uint8

const (
	// Generic flags
	FlagNone       FrameFlags = 0x0
	FlagEndStream  FrameFlags = 0x1  // Last frame for stream
	FlagAck        FrameFlags = 0x1  // Acknowledgment (for PING/SETTINGS)
	FlagEndHeaders FrameFlags = 0x4  // End of headers
	FlagPadded     FrameFlags = 0x8  // Frame is padded
	FlagPriority   FrameFlags = 0x20 // Frame contains priority info
)

// Frame format (HTTP/2 compatible):
// +-----------------------------------------------+
// |                 Length (24)                   |
// +---------------+---------------+---------------+
// |   Type (8)    |   Flags (8)   |
// +-+-------------+---------------+-------------------------------+
// |R|                 Stream Identifier (31)                     |
// +=+=============================================================+
// |                   Frame Payload (0...)                     ...
// +---------------------------------------------------------------+

const (
	frameHeaderLen = 9
	maxFrameLen    = 1<<24 - 1 // 16MB (HTTP/2 max)
)

// WriteFrame writes a frame to the writer with proper error handling
func WriteFrame(w io.Writer, f *Frame) error {
	if f == nil {
		return fmt.Errorf("frame cannot be nil")
	}

	if len(f.Data) > maxFrameLen {
		return fmt.Errorf("frame payload too large: %d bytes (max %d)", len(f.Data), maxFrameLen)
	}

	header := make([]byte, frameHeaderLen)

	// Length (24 bits) - properly encode the length
	dataLen := uint32(len(f.Data))
	header[0] = byte(dataLen >> 16)
	header[1] = byte(dataLen >> 8)
	header[2] = byte(dataLen)

	// Type (8 bits)
	header[3] = uint8(f.Type)

	// Flags (8 bits)
	header[4] = uint8(f.Flags)

	// Stream ID (31 bits, R bit is 0)
	binary.BigEndian.PutUint32(header[5:9], f.StreamID&0x7fffffff)

	// Write header atomically
	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("failed to write frame header: %w", err)
	}

	// Write data if present
	if len(f.Data) > 0 {
		if _, err := w.Write(f.Data); err != nil {
			return fmt.Errorf("failed to write frame data: %w", err)
		}
	}

	return nil
}

// ReadFrame reads a frame from the reader with better error handling
func ReadFrame(r io.Reader) (*Frame, error) {
	header := make([]byte, frameHeaderLen)
	if _, err := io.ReadFull(r, header); err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read frame header: %w", err)
	}

	// Parse length (24 bits)
	length := uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2])
	if length > maxFrameLen {
		return nil, fmt.Errorf("frame payload too large: %d bytes (max %d)", length, maxFrameLen)
	}

	// Parse type (8 bits)
	frameType := FrameType(header[3])

	// Parse flags (8 bits)
	flags := FrameFlags(header[4])

	// Parse stream ID (31 bits)
	streamID := binary.BigEndian.Uint32(header[5:9]) & 0x7fffffff

	// Read data
	var data []byte
	if length > 0 {
		data = make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("failed to read frame data: %w", err)
		}
	}

	return &Frame{
		StreamID: streamID,
		Type:     frameType,
		Flags:    flags,
		Data:     data,
	}, nil
}

// Validate checks if the frame is valid according to protocol rules
func (f *Frame) Validate() error {
	if f == nil {
		return fmt.Errorf("frame cannot be nil")
	}

	// Check frame size
	if len(f.Data) > maxFrameLen {
		return fmt.Errorf("frame payload too large: %d bytes", len(f.Data))
	}

	// Stream ID validation
	switch f.Type {
	case FrameTypeSettings, FrameTypePing, FrameTypeGoAway:
		// Connection-level frames must have stream ID 0
		if f.StreamID != 0 {
			return fmt.Errorf("connection-level frame cannot have non-zero stream ID: %d", f.StreamID)
		}
	case FrameTypeData, FrameTypeHeaders, FrameTypePriority, FrameTypeRstStream, FrameTypeWindowUpdate:
		// Stream-level frames must have non-zero stream ID
		if f.StreamID == 0 {
			return fmt.Errorf("stream-level frame must have non-zero stream ID")
		}
	}

	// Type-specific validation
	switch f.Type {
	case FrameTypeData:
		// DATA frames can have END_STREAM flag
		if f.Flags&^(FlagEndStream|FlagPadded) != 0 {
			return fmt.Errorf("invalid flags for DATA frame: %02x", f.Flags)
		}
	case FrameTypeHeaders:
		// HEADERS frames can have multiple flags
		validFlags := FlagEndStream | FlagEndHeaders | FlagPadded | FlagPriority
		if f.Flags&^validFlags != 0 {
			return fmt.Errorf("invalid flags for HEADERS frame: %02x", f.Flags)
		}
	case FrameTypePing:
		// PING frames must be exactly 8 bytes
		if len(f.Data) != 8 {
			return fmt.Errorf("PING frame must be exactly 8 bytes, got %d", len(f.Data))
		}
		// PING frames can only have ACK flag
		if f.Flags&^FlagAck != 0 {
			return fmt.Errorf("invalid flags for PING frame: %02x", f.Flags)
		}
	case FrameTypeSettings:
		// SETTINGS frames payload must be multiple of 6 bytes
		if len(f.Data)%6 != 0 {
			return fmt.Errorf("SETTINGS frame payload must be multiple of 6 bytes, got %d", len(f.Data))
		}
		// SETTINGS frames can only have ACK flag
		if f.Flags&^FlagAck != 0 {
			return fmt.Errorf("invalid flags for SETTINGS frame: %02x", f.Flags)
		}
	case FrameTypeWindowUpdate:
		// WINDOW_UPDATE frames must be exactly 4 bytes
		if len(f.Data) != 4 {
			return fmt.Errorf("WINDOW_UPDATE frame must be exactly 4 bytes, got %d", len(f.Data))
		}
		// Check that the increment is not zero
		if len(f.Data) == 4 {
			increment := binary.BigEndian.Uint32(f.Data) & 0x7fffffff
			if increment == 0 {
				return fmt.Errorf("WINDOW_UPDATE increment cannot be zero")
			}
		}
	case FrameTypeRstStream:
		// RST_STREAM frames must be exactly 4 bytes
		if len(f.Data) != 4 {
			return fmt.Errorf("RST_STREAM frame must be exactly 4 bytes, got %d", len(f.Data))
		}
	case FrameTypeGoAway:
		// GOAWAY frames must be at least 8 bytes
		if len(f.Data) < 8 {
			return fmt.Errorf("GOAWAY frame must be at least 8 bytes, got %d", len(f.Data))
		}
	}

	return nil
}

// String returns a detailed string representation of the frame
func (f *Frame) String() string {
	if f == nil {
		return "Frame{nil}"
	}
	return fmt.Sprintf("Frame{StreamID: %d, Type: %v, Flags: %v, DataLen: %d}",
		f.StreamID, f.Type, f.Flags, len(f.Data))
}

// String returns a string representation of the frame type
func (t FrameType) String() string {
	switch t {
	case FrameTypeData:
		return "DATA"
	case FrameTypeHeaders:
		return "HEADERS"
	case FrameTypePriority:
		return "PRIORITY"
	case FrameTypeRstStream:
		return "RST_STREAM"
	case FrameTypeSettings:
		return "SETTINGS"
	case FrameTypePushPromise:
		return "PUSH_PROMISE"
	case FrameTypePing:
		return "PING"
	case FrameTypeGoAway:
		return "GOAWAY"
	case FrameTypeWindowUpdate:
		return "WINDOW_UPDATE"
	case FrameTypeContinuation:
		return "CONTINUATION"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", uint8(t))
	}
}

// String returns a string representation of the frame flags
func (f FrameFlags) String() string {
	if f == FlagNone {
		return "NONE"
	}

	var flags []string
	if f&FlagEndStream != 0 {
		flags = append(flags, "END_STREAM")
	}
	// Note: FlagAck has the same value as FlagEndStream (0x1)
	// Only show ACK for PING and SETTINGS frames where it makes sense
	if f&FlagEndHeaders != 0 {
		flags = append(flags, "END_HEADERS")
	}
	if f&FlagPadded != 0 {
		flags = append(flags, "PADDED")
	}
	if f&FlagPriority != 0 {
		flags = append(flags, "PRIORITY")
	}

	if len(flags) == 0 {
		return fmt.Sprintf("0x%02x", uint8(f))
	}

	result := flags[0]
	for i := 1; i < len(flags); i++ {
		result += "|" + flags[i]
	}
	return result
}

// NewDataFrame creates a new DATA frame
func NewDataFrame(streamID uint32, data []byte, endStream bool) *Frame {
	flags := FlagNone
	if endStream {
		flags |= FlagEndStream
	}
	return &Frame{
		StreamID: streamID,
		Type:     FrameTypeData,
		Flags:    flags,
		Data:     data,
	}
}

// NewRstStreamFrame creates a new RST_STREAM frame
func NewRstStreamFrame(streamID uint32, errorCode ErrorCode) *Frame {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(errorCode))
	return &Frame{
		StreamID: streamID,
		Type:     FrameTypeRstStream,
		Flags:    FlagNone,
		Data:     data,
	}
}

// NewWindowUpdateFrame creates a new WINDOW_UPDATE frame
func NewWindowUpdateFrame(streamID uint32, increment uint32) *Frame {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, increment&0x7fffffff)
	return &Frame{
		StreamID: streamID,
		Type:     FrameTypeWindowUpdate,
		Flags:    FlagNone,
		Data:     data,
	}
}

// NewPingFrame creates a new PING frame
func NewPingFrame(data []byte, ack bool) *Frame {
	flags := FlagNone
	if ack {
		flags |= FlagAck
	}
	pingData := make([]byte, 8)
	copy(pingData, data)
	return &Frame{
		StreamID: 0,
		Type:     FrameTypePing,
		Flags:    flags,
		Data:     pingData,
	}
}

// NewGoAwayFrame creates a new GOAWAY frame
func NewGoAwayFrame(lastStreamID uint32, errorCode ErrorCode, debugData []byte) *Frame {
	data := make([]byte, 8+len(debugData))
	binary.BigEndian.PutUint32(data[0:4], lastStreamID&0x7fffffff)
	binary.BigEndian.PutUint32(data[4:8], uint32(errorCode))
	copy(data[8:], debugData)
	return &Frame{
		StreamID: 0,
		Type:     FrameTypeGoAway,
		Flags:    FlagNone,
		Data:     data,
	}
}
