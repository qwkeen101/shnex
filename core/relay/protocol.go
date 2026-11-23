package relay

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// MagicHeader is sent at the start of both directions on a shnex-agent mux stream
// so mismatched binaries or shell "command not found" errors are caught deterministically.
const MagicHeader = "SHNEX01\n"

const MaxPacketPayload = 256 * 1024

// Packet opcodes for the shnex-agent framed binary stream.
const (
	PktHello   byte = 0x01 // Client -> Daemon: HelloPayload JSON
	PktWelcome byte = 0x02 // Daemon -> Client: WelcomePayload JSON
	PktData    byte = 0x03 // Bidirectional: raw PTY stdin/stdout bytes
	PktResize  byte = 0x04 // Client -> Daemon: [cols:u16 BE][rows:u16 BE]
	PktPing    byte = 0x05 // Client -> Daemon: [seq:u64 BE]
	PktPong    byte = 0x06 // Daemon -> Client: [seq:u64 BE]
	PktExit    byte = 0x07 // Daemon -> Client: ExitPayload JSON
	PktListReq byte = 0x08 // CLI -> Daemon: list active persistent workspaces
	PktListRes byte = 0x09 // Daemon -> CLI: []WorkspaceInfo JSON
	PktKillReq byte = 0x0A // CLI -> Daemon: kill workspace by name
)

var ErrInvalidMagic = errors.New("relay: invalid shnex-agent protocol magic header (is shnex-agent installed on remote host?)")

// HelloPayload requests creating or attaching to a named persistent workspace.
type HelloPayload struct {
	SessionName string `json:"session_name"`
	Cols        uint16 `json:"cols"`
	Rows        uint16 `json:"rows"`
	Term        string `json:"term,omitempty"`
	Command     string `json:"command,omitempty"`
}

// WelcomePayload reports metadata about the attached remote persistent workspace.
type WelcomePayload struct {
	SessionName   string    `json:"session_name"`
	PID           int       `json:"pid"`
	CreatedAt     time.Time `json:"created_at"`
	AttachedPeers int       `json:"attached_peers"`
	ReplayedBytes int       `json:"replayed_bytes"`
	NewlyCreated  bool      `json:"newly_created"`
}

// ExitPayload notifies attached clients that the underlying shell/command exited.
type ExitPayload struct {
	ExitCode int    `json:"exit_code"`
	Message  string `json:"message,omitempty"`
}

// WorkspaceInfo summarizes a persistent workspace hosted by shnex-agent.
type WorkspaceInfo struct {
	SessionName   string    `json:"session_name"`
	PID           int       `json:"pid"`
	Cols          uint16    `json:"cols"`
	Rows          uint16    `json:"rows"`
	AttachedPeers int       `json:"attached_peers"`
	CreatedAt     time.Time `json:"created_at"`
}

// StreamWriter serializes framed packets thread-safely onto an io.Writer.
type StreamWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func NewStreamWriter(w io.Writer) *StreamWriter {
	return &StreamWriter{w: w}
}

func (sw *StreamWriter) WriteMagic() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	_, err := io.WriteString(sw.w, MagicHeader)
	return err
}

func (sw *StreamWriter) WritePacket(pktType byte, payload []byte) error {
	if len(payload) > MaxPacketPayload {
		return fmt.Errorf("relay: packet payload %d exceeds max %d", len(payload), MaxPacketPayload)
	}
	var hdr [5]byte
	hdr[0] = pktType
	binary.BigEndian.PutUint32(hdr[1:5], uint32(len(payload)))

	sw.mu.Lock()
	defer sw.mu.Unlock()
	if _, err := sw.w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := sw.w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func (sw *StreamWriter) WriteJSON(pktType byte, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return sw.WritePacket(pktType, raw)
}

func (sw *StreamWriter) WriteResize(cols, rows uint16) error {
	var b [4]byte
	binary.BigEndian.PutUint16(b[0:2], cols)
	binary.BigEndian.PutUint16(b[2:4], rows)
	return sw.WritePacket(PktResize, b[:])
}

// ReadMagic verifies the 8-byte "SHNEX01\n" stream greeting.
func ReadMagic(r io.Reader) error {
	var buf [len(MagicHeader)]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMagic, err)
	}
	if string(buf[:]) != MagicHeader {
		return fmt.Errorf("%w (got %q)", ErrInvalidMagic, string(buf[:]))
	}
	return nil
}

// ReadPacket reads a single [1B type][4B len][payload] frame from r.
func ReadPacket(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	pktType := hdr[0]
	length := binary.BigEndian.Uint32(hdr[1:5])
	if length > MaxPacketPayload {
		return 0, nil, fmt.Errorf("relay: oversized packet length %d", length)
	}
	if length == 0 {
		return pktType, nil, nil
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return pktType, payload, nil
}

func DecodeResizePayload(payload []byte) (cols, rows uint16, err error) {
	if len(payload) < 4 {
		return 0, 0, errors.New("relay: invalid resize payload")
	}
	return binary.BigEndian.Uint16(payload[0:2]), binary.BigEndian.Uint16(payload[2:4]), nil
}
