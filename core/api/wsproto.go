package api

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shnex/shnex/core/transport"
)

// Binary WebSocket frame opcodes (1-byte header).
const (
	FrameData   byte = 0x00 // Bidirectional raw PTY bytes: [0x00][payload...]
	FrameResize byte = 0x01 // Client -> Server window resize: [0x01][cols:u16][rows:u16]([widthPx:u16][heightPx:u16])
	FramePing   byte = 0x02 // Client -> Server keepalive & RTT probe: [0x02][clientTimestamp:u64 optional]
	FramePong   byte = 0x03 // Server -> Client keepalive response: [0x03][clientTimestamp:u64][sshRttMicros:u64]
	FrameEvent  byte = 0x04 // Server -> Client structured SessionEvent JSON: [0x04][utf8-json...]
)

var (
	ErrEmptyFrame        = errors.New("wsproto: empty binary frame")
	ErrInvalidResizeSize = errors.New("wsproto: resize frame requires 5 or 9 bytes")
)

// EncodeDataFrame prefixes raw PTY bytes with FrameData (0x00).
func EncodeDataFrame(payload []byte) []byte {
	frame := make([]byte, 1+len(payload))
	frame[0] = FrameData
	copy(frame[1:], payload)
	return frame
}

// EncodeResizeFrame encodes a TerminalSize into a binary FrameResize message.
func EncodeResizeFrame(size transport.TerminalSize) []byte {
	if size.WidthPx > 0 || size.HeightPx > 0 {
		buf := make([]byte, 9)
		buf[0] = FrameResize
		binary.BigEndian.PutUint16(buf[1:3], size.Cols)
		binary.BigEndian.PutUint16(buf[3:5], size.Rows)
		binary.BigEndian.PutUint16(buf[5:7], size.WidthPx)
		binary.BigEndian.PutUint16(buf[7:9], size.HeightPx)
		return buf
	}
	buf := make([]byte, 5)
	buf[0] = FrameResize
	binary.BigEndian.PutUint16(buf[1:3], size.Cols)
	binary.BigEndian.PutUint16(buf[3:5], size.Rows)
	return buf
}

// DecodeResizeFrame parses a FrameResize binary message (including leading opcode byte).
func DecodeResizeFrame(frame []byte) (transport.TerminalSize, error) {
	if len(frame) == 0 || frame[0] != FrameResize {
		return transport.TerminalSize{}, fmt.Errorf("wsproto: expected opcode 0x%02x", FrameResize)
	}
	switch len(frame) {
	case 5:
		return transport.TerminalSize{
			Cols: binary.BigEndian.Uint16(frame[1:3]),
			Rows: binary.BigEndian.Uint16(frame[3:5]),
		}, nil
	case 9:
		return transport.TerminalSize{
			Cols:     binary.BigEndian.Uint16(frame[1:3]),
			Rows:     binary.BigEndian.Uint16(frame[3:5]),
			WidthPx:  binary.BigEndian.Uint16(frame[5:7]),
			HeightPx: binary.BigEndian.Uint16(frame[7:9]),
		}, nil
	default:
		return transport.TerminalSize{}, ErrInvalidResizeSize
	}
}

// EncodePongFrame encodes clientTimestamp and measured SSH RTT (in microseconds).
func EncodePongFrame(clientTs uint64, rttMicros uint64) []byte {
	buf := make([]byte, 17)
	buf[0] = FramePong
	binary.BigEndian.PutUint64(buf[1:9], clientTs)
	binary.BigEndian.PutUint64(buf[9:17], rttMicros)
	return buf
}

// DecodePingTimestamp extracts the optional 8-byte client timestamp from a FramePing payload.
func DecodePingTimestamp(frame []byte) uint64 {
	if len(frame) >= 9 && frame[0] == FramePing {
		return binary.BigEndian.Uint64(frame[1:9])
	}
	return 0
}

// EncodeEventFrame serializes a transport.SessionEvent with leading FrameEvent (0x04).
func EncodeEventFrame(ev transport.SessionEvent) ([]byte, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 1+len(raw))
	buf[0] = FrameEvent
	copy(buf[1:], raw)
	return buf, nil
}
