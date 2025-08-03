package mux

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestFrame_Validate(t *testing.T) {
	tests := []struct {
		name    string
		frame   *Frame
		wantErr bool
	}{
		{
			name:    "nil frame",
			frame:   nil,
			wantErr: true,
		},
		{
			name: "valid DATA frame",
			frame: &Frame{
				StreamID: 1,
				Type:     FrameTypeData,
				Flags:    FlagEndStream,
				Data:     []byte("hello"),
			},
			wantErr: false,
		},
		{
			name: "DATA frame with stream ID 0",
			frame: &Frame{
				StreamID: 0,
				Type:     FrameTypeData,
				Flags:    0,
				Data:     []byte("hello"),
			},
			wantErr: true,
		},
		{
			name: "valid PING frame",
			frame: &Frame{
				StreamID: 0,
				Type:     FrameTypePing,
				Flags:    0,
				Data:     make([]byte, 8),
			},
			wantErr: false,
		},
		{
			name: "PING frame with wrong size",
			frame: &Frame{
				StreamID: 0,
				Type:     FrameTypePing,
				Flags:    0,
				Data:     make([]byte, 4),
			},
			wantErr: true,
		},
		{
			name: "PING frame with non-zero stream ID",
			frame: &Frame{
				StreamID: 1,
				Type:     FrameTypePing,
				Flags:    0,
				Data:     make([]byte, 8),
			},
			wantErr: true,
		},
		{
			name: "valid WINDOW_UPDATE frame",
			frame: &Frame{
				StreamID: 1,
				Type:     FrameTypeWindowUpdate,
				Flags:    0,
				Data:     []byte{0x00, 0x00, 0x10, 0x00}, // 4096
			},
			wantErr: false,
		},
		{
			name: "WINDOW_UPDATE frame with zero increment",
			frame: &Frame{
				StreamID: 1,
				Type:     FrameTypeWindowUpdate,
				Flags:    0,
				Data:     []byte{0x00, 0x00, 0x00, 0x00}, // 0
			},
			wantErr: true,
		},
		{
			name: "frame too large",
			frame: &Frame{
				StreamID: 1,
				Type:     FrameTypeData,
				Flags:    0,
				Data:     make([]byte, maxFrameLen+1),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.frame.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Frame.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWriteReadFrame(t *testing.T) {
	tests := []struct {
		name  string
		frame *Frame
	}{
		{
			name: "DATA frame",
			frame: &Frame{
				StreamID: 1,
				Type:     FrameTypeData,
				Flags:    FlagEndStream,
				Data:     []byte("hello world"),
			},
		},
		{
			name: "PING frame",
			frame: &Frame{
				StreamID: 0,
				Type:     FrameTypePing,
				Flags:    FlagAck,
				Data:     []byte("12345678"),
			},
		},
		{
			name:  "WINDOW_UPDATE frame",
			frame: NewWindowUpdateFrame(5, 4096),
		},
		{
			name:  "RST_STREAM frame",
			frame: NewRstStreamFrame(3, ErrorCancel),
		},
		{
			name:  "GOAWAY frame",
			frame: NewGoAwayFrame(7, ErrorProtocolError, []byte("protocol error")),
		},
		{
			name: "empty DATA frame",
			frame: &Frame{
				StreamID: 2,
				Type:     FrameTypeData,
				Flags:    0,
				Data:     nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write frame to buffer
			var buf bytes.Buffer
			err := WriteFrame(&buf, tt.frame)
			if err != nil {
				t.Fatalf("WriteFrame() error = %v", err)
			}

			// Read frame back
			readFrame, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame() error = %v", err)
			}

			// Compare frames
			if !reflect.DeepEqual(tt.frame, readFrame) {
				t.Errorf("Frame mismatch:\noriginal: %+v\nread:     %+v", tt.frame, readFrame)
			}
		})
	}
}

func TestFrameString(t *testing.T) {
	frame := &Frame{
		StreamID: 5,
		Type:     FrameTypeData,
		Flags:    FlagEndStream,
		Data:     []byte("test"),
	}

	expected := "Frame{StreamID: 5, Type: DATA, Flags: END_STREAM, DataLen: 4}"
	if got := frame.String(); got != expected {
		t.Errorf("Frame.String() = %q, want %q", got, expected)
	}
}

func TestFrameTypeString(t *testing.T) {
	tests := []struct {
		frameType FrameType
		expected  string
	}{
		{FrameTypeData, "DATA"},
		{FrameTypeHeaders, "HEADERS"},
		{FrameTypePing, "PING"},
		{FrameTypeGoAway, "GOAWAY"},
		{FrameType(99), "UNKNOWN(99)"},
	}

	for _, tt := range tests {
		if got := tt.frameType.String(); got != tt.expected {
			t.Errorf("FrameType(%d).String() = %q, want %q", tt.frameType, got, tt.expected)
		}
	}
}

func TestFrameFlagsString(t *testing.T) {
	tests := []struct {
		flags    FrameFlags
		expected string
	}{
		{FlagNone, "NONE"},
		{FlagEndStream, "END_STREAM"},
		{FlagEndHeaders, "END_HEADERS"},
		{FlagEndStream | FlagPadded, "END_STREAM|PADDED"},
		{FlagEndStream | FlagEndHeaders | FlagPadded | FlagPriority, "END_STREAM|END_HEADERS|PADDED|PRIORITY"},
	}

	for _, tt := range tests {
		if got := tt.flags.String(); got != tt.expected {
			t.Errorf("FrameFlags(%d).String() = %q, want %q", tt.flags, got, tt.expected)
		}
	}
}

func TestNewFrameFunctions(t *testing.T) {
	t.Run("NewDataFrame", func(t *testing.T) {
		data := []byte("test data")
		frame := NewDataFrame(5, data, true)

		if frame.StreamID != 5 {
			t.Errorf("StreamID = %d, want 5", frame.StreamID)
		}
		if frame.Type != FrameTypeData {
			t.Errorf("Type = %v, want %v", frame.Type, FrameTypeData)
		}
		if frame.Flags != FlagEndStream {
			t.Errorf("Flags = %v, want %v", frame.Flags, FlagEndStream)
		}
		if !bytes.Equal(frame.Data, data) {
			t.Errorf("Data = %v, want %v", frame.Data, data)
		}
	})

	t.Run("NewRstStreamFrame", func(t *testing.T) {
		frame := NewRstStreamFrame(3, ErrorCancel)

		if frame.StreamID != 3 {
			t.Errorf("StreamID = %d, want 3", frame.StreamID)
		}
		if frame.Type != FrameTypeRstStream {
			t.Errorf("Type = %v, want %v", frame.Type, FrameTypeRstStream)
		}
		if len(frame.Data) != 4 {
			t.Errorf("Data length = %d, want 4", len(frame.Data))
		}

		errorCode := ErrorCode(binary.BigEndian.Uint32(frame.Data))
		if errorCode != ErrorCancel {
			t.Errorf("Error code = %v, want %v", errorCode, ErrorCancel)
		}
	})

	t.Run("NewWindowUpdateFrame", func(t *testing.T) {
		frame := NewWindowUpdateFrame(7, 4096)

		if frame.StreamID != 7 {
			t.Errorf("StreamID = %d, want 7", frame.StreamID)
		}
		if frame.Type != FrameTypeWindowUpdate {
			t.Errorf("Type = %v, want %v", frame.Type, FrameTypeWindowUpdate)
		}
		if len(frame.Data) != 4 {
			t.Errorf("Data length = %d, want 4", len(frame.Data))
		}

		increment := binary.BigEndian.Uint32(frame.Data) & 0x7fffffff
		if increment != 4096 {
			t.Errorf("Increment = %d, want 4096", increment)
		}
	})

	t.Run("NewPingFrame", func(t *testing.T) {
		pingData := []byte("pingdata")
		frame := NewPingFrame(pingData, true)

		if frame.StreamID != 0 {
			t.Errorf("StreamID = %d, want 0", frame.StreamID)
		}
		if frame.Type != FrameTypePing {
			t.Errorf("Type = %v, want %v", frame.Type, FrameTypePing)
		}
		if frame.Flags != FlagAck {
			t.Errorf("Flags = %v, want %v", frame.Flags, FlagAck)
		}
		if len(frame.Data) != 8 {
			t.Errorf("Data length = %d, want 8", len(frame.Data))
		}
		if !bytes.Equal(frame.Data[:len(pingData)], pingData) {
			t.Errorf("Data = %v, want %v", frame.Data[:len(pingData)], pingData)
		}
	})

	t.Run("NewGoAwayFrame", func(t *testing.T) {
		debugData := []byte("shutdown")
		frame := NewGoAwayFrame(15, ErrorInternalError, debugData)

		if frame.StreamID != 0 {
			t.Errorf("StreamID = %d, want 0", frame.StreamID)
		}
		if frame.Type != FrameTypeGoAway {
			t.Errorf("Type = %v, want %v", frame.Type, FrameTypeGoAway)
		}
		if len(frame.Data) != 8+len(debugData) {
			t.Errorf("Data length = %d, want %d", len(frame.Data), 8+len(debugData))
		}

		lastStreamID := binary.BigEndian.Uint32(frame.Data[0:4]) & 0x7fffffff
		if lastStreamID != 15 {
			t.Errorf("Last stream ID = %d, want 15", lastStreamID)
		}

		errorCode := ErrorCode(binary.BigEndian.Uint32(frame.Data[4:8]))
		if errorCode != ErrorInternalError {
			t.Errorf("Error code = %v, want %v", errorCode, ErrorInternalError)
		}

		if !bytes.Equal(frame.Data[8:], debugData) {
			t.Errorf("Debug data = %v, want %v", frame.Data[8:], debugData)
		}
	})
}

func BenchmarkWriteFrame(b *testing.B) {
	frame := &Frame{
		StreamID: 1,
		Type:     FrameTypeData,
		Flags:    0,
		Data:     make([]byte, 1024),
	}

	var buf bytes.Buffer
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		WriteFrame(&buf, frame)
	}
}

func BenchmarkReadFrame(b *testing.B) {
	frame := &Frame{
		StreamID: 1,
		Type:     FrameTypeData,
		Flags:    0,
		Data:     make([]byte, 1024),
	}

	var buf bytes.Buffer
	WriteFrame(&buf, frame)
	frameData := buf.Bytes()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := bytes.NewReader(frameData)
		ReadFrame(buf)
	}
}
