package mux

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Frame represents a multiplexing frame similar to HTTP/2
type Frame struct {
	StreamID uint32
	Type     FrameType
	Flags    FrameFlags
	Data     []byte
}

// FrameType represents different types of frames
type FrameType uint8

const (
	// Data frame carries application data for a stream
	FrameTypeData FrameType = 0x0
	// StreamOpen opens a new stream
	FrameTypeStreamOpen FrameType = 0x1
	// StreamClose closes a stream
	FrameTypeStreamClose FrameType = 0x2
	// WindowUpdate provides flow control
	FrameTypeWindowUpdate FrameType = 0x3
	// Ping for keepalive and RTT measurement
	FrameTypePing FrameType = 0x4
	// Settings for connection-level configuration
	FrameTypeSettings FrameType = 0x5
)

// FrameFlags represents frame flags
type FrameFlags uint8

const (
	// FlagEndStream indicates this is the last frame for the stream
	FlagEndStream FrameFlags = 0x1
	// FlagAck indicates this is an acknowledgment frame
	FlagAck FrameFlags = 0x2
)

// Frame format (inspired by HTTP/2):
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
	maxFrameLen    = 1<<24 - 1 // 16MB
)

// WriteFrame writes a frame to the writer
func WriteFrame(w io.Writer, f *Frame) error {
	if len(f.Data) > maxFrameLen {
		return fmt.Errorf("frame too large: %d bytes", len(f.Data))
	}

	header := make([]byte, frameHeaderLen)

	// Length (24 bits)
	binary.BigEndian.PutUint32(header[0:4], uint32(len(f.Data)))
	header[0] = header[1] // Move the length to the first 3 bytes
	header[1] = header[2]
	header[2] = header[3]

	// Type (8 bits)
	header[3] = uint8(f.Type)

	// Flags (8 bits)
	header[4] = uint8(f.Flags)

	// Stream ID (31 bits, R bit is 0)
	binary.BigEndian.PutUint32(header[5:9], f.StreamID&0x7fffffff)

	// Write header
	if _, err := w.Write(header); err != nil {
		return err
	}

	// Write data
	if len(f.Data) > 0 {
		if _, err := w.Write(f.Data); err != nil {
			return err
		}
	}

	return nil
}

// ReadFrame reads a frame from the reader
func ReadFrame(r io.Reader) (*Frame, error) {
	header := make([]byte, frameHeaderLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	// Parse length (24 bits)
	length := uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2])
	if length > maxFrameLen {
		return nil, fmt.Errorf("frame too large: %d bytes", length)
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
			return nil, err
		}
	}

	return &Frame{
		StreamID: streamID,
		Type:     frameType,
		Flags:    flags,
		Data:     data,
	}, nil
}

// String returns a string representation of the frame
func (f *Frame) String() string {
	return fmt.Sprintf("Frame{StreamID: %d, Type: %v, Flags: %v, DataLen: %d}",
		f.StreamID, f.Type, f.Flags, len(f.Data))
}

// String returns a string representation of the frame type
func (t FrameType) String() string {
	switch t {
	case FrameTypeData:
		return "DATA"
	case FrameTypeStreamOpen:
		return "STREAM_OPEN"
	case FrameTypeStreamClose:
		return "STREAM_CLOSE"
	case FrameTypeWindowUpdate:
		return "WINDOW_UPDATE"
	case FrameTypePing:
		return "PING"
	case FrameTypeSettings:
		return "SETTINGS"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", uint8(t))
	}
}

// String returns a string representation of the frame flags
func (f FrameFlags) String() string {
	var flags []string
	if f&FlagEndStream != 0 {
		flags = append(flags, "END_STREAM")
	}
	if f&FlagAck != 0 {
		flags = append(flags, "ACK")
	}
	if len(flags) == 0 {
		return "NONE"
	}
	return fmt.Sprintf("[%s]", flags[0])
}
