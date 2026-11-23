package ssh

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shnex/shnex/core/transport"
)

type activeForward struct {
	rule     transport.PortForwardRule
	listener net.Listener
	stopOnce sync.Once
}

func (af *activeForward) close() {
	af.stopOnce.Do(func() {
		_ = af.listener.Close()
	})
}

// StartLocalForward binds 127.0.0.1:<localPort> (or ephemeral port when 0) and tunnels connections
// over the multiplexed SSH session to remoteAddr (e.g. "127.0.0.1:3000").
func (s *Session) StartLocalForward(ctx context.Context, localPort int, remoteAddr string) (transport.PortForwardRule, error) {
	if err := ctx.Err(); err != nil {
		return transport.PortForwardRule{}, err
	}
	remoteAddr = strings.TrimSpace(remoteAddr)
	if _, _, err := net.SplitHostPort(remoteAddr); err != nil {
		return transport.PortForwardRule{}, fmt.Errorf("ssh forward: invalid remote address %q (expected host:port)", remoteAddr)
	}
	if localPort < 0 || localPort > 65535 {
		return transport.PortForwardRule{}, errors.New("ssh forward: local port out of range")
	}

	bindAddr := "127.0.0.1:" + strconv.Itoa(localPort)
	ln, err := net.Listen("tcp4", bindAddr)
	if err != nil {
		return transport.PortForwardRule{}, fmt.Errorf("ssh forward: listen %s: %w", bindAddr, err)
	}

	var idBytes [4]byte
	_, _ = rand.Read(idBytes[:])
	rule := transport.PortForwardRule{
		ID:         "fwd-" + hex.EncodeToString(idBytes[:]),
		LocalAddr:  ln.Addr().String(),
		RemoteAddr: remoteAddr,
		CreatedAt:  time.Now(),
	}
	af := &activeForward{
		rule:     rule,
		listener: ln,
	}

	s.fwdMu.Lock()
	if s.forwards == nil {
		s.forwards = make(map[string]*activeForward)
	}
	s.forwards[rule.ID] = af
	s.fwdMu.Unlock()

	go s.acceptForwardLoop(af)
	return rule, nil
}

func (s *Session) acceptForwardLoop(af *activeForward) {
	for {
		localConn, err := af.listener.Accept()
		if err != nil {
			return
		}
		go func(lc net.Conn) {
			defer lc.Close()
			rc, dErr := s.client.Dial("tcp", af.rule.RemoteAddr)
			if dErr != nil {
				return
			}
			defer rc.Close()

			done := make(chan struct{}, 2)
			go func() {
				_, _ = io.Copy(rc, lc)
				done <- struct{}{}
			}()
			go func() {
				_, _ = io.Copy(lc, rc)
				done <- struct{}{}
			}()
			<-done
		}(localConn)
	}
}

func (s *Session) ListForwards() []transport.PortForwardRule {
	s.fwdMu.Lock()
	out := make([]transport.PortForwardRule, 0, len(s.forwards))
	for _, af := range s.forwards {
		out = append(out, af.rule)
	}
	s.fwdMu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *Session) StopForward(id string) error {
	s.fwdMu.Lock()
	af, ok := s.forwards[id]
	if ok {
		delete(s.forwards, id)
	}
	s.fwdMu.Unlock()
	if !ok {
		return errors.New("port forward not found")
	}
	af.close()
	return nil
}

func (s *Session) closeAllForwards() {
	s.fwdMu.Lock()
	list := make([]*activeForward, 0, len(s.forwards))
	for id, af := range s.forwards {
		list = append(list, af)
		delete(s.forwards, id)
	}
	s.fwdMu.Unlock()
	for _, af := range list {
		af.close()
	}
}
