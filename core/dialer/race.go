package dialer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// RaceCandidate is one route a RaceDialer may use (a proxy from the pool, or direct).
type RaceCandidate struct {
	Key    string // stable identity, e.g. the proxy id or "direct"
	Name   string // shown to the user, e.g. "香港"
	Dialer Dialer
}

const (
	defaultRaceStagger  = 3 * time.Second
	defaultRaceTimeout  = 15 * time.Second
	raceWinnerMemoryTTL = 5 * time.Minute
	maxBannerBytes      = 8 * 1024
)

// RaceDialer tries its candidates in order, starting the next one when the current one
// fails or has not produced a usable connection within Stagger; the first usable
// connection wins and the others are closed.
//
// With ExpectSSHBanner a connection only counts once the server's "SSH-" identification
// line has been read: proxies such as xray return from Dial before the far end is reached,
// so a dead node is otherwise only noticed during the SSH handshake. The bytes read are
// replayed to the caller.
//
// The winner of each candidate set is remembered in memory for a few minutes and tried
// first next time, so reconnecting after the primary died goes straight to the backup.
type RaceDialer struct {
	Candidates      []RaceCandidate
	Stagger         time.Duration
	Timeout         time.Duration // per candidate, including the banner read
	ExpectSSHBanner bool

	mu     sync.Mutex
	winner RaceCandidate
}

// NewRaceDialer builds a RaceDialer with default stagger and timeout.
func NewRaceDialer(candidates []RaceCandidate, expectSSHBanner bool) *RaceDialer {
	return &RaceDialer{
		Candidates:      candidates,
		Stagger:         defaultRaceStagger,
		Timeout:         defaultRaceTimeout,
		ExpectSSHBanner: expectSSHBanner,
	}
}

// Winner reports the candidate used by the last successful Dial and whether it was not
// the first configured one (a fallback happened).
func (d *RaceDialer) Winner() (c RaceCandidate, fallback bool, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.winner.Key == "" {
		return RaceCandidate{}, false, false
	}
	return d.winner, len(d.Candidates) > 0 && d.winner.Key != d.Candidates[0].Key, true
}

var raceMemory = struct {
	sync.Mutex
	m map[string]raceMemo
}{m: map[string]raceMemo{}}

type raceMemo struct {
	key string
	at  time.Time
}

func raceSetKey(cands []RaceCandidate) string {
	keys := make([]string, len(cands))
	for i, c := range cands {
		keys[i] = c.Key
	}
	return strings.Join(keys, "\x00")
}

func rememberWinner(set, key string, now time.Time) {
	raceMemory.Lock()
	raceMemory.m[set] = raceMemo{key: key, at: now}
	raceMemory.Unlock()
}

func recalledWinner(set string, now time.Time) string {
	raceMemory.Lock()
	defer raceMemory.Unlock()
	m, ok := raceMemory.m[set]
	if !ok || now.Sub(m.at) > raceWinnerMemoryTTL {
		delete(raceMemory.m, set)
		return ""
	}
	return m.key
}

// orderCandidates moves the remembered winner to the front, keeping the rest in order.
func orderCandidates(cands []RaceCandidate, first string) []RaceCandidate {
	out := make([]RaceCandidate, 0, len(cands))
	for _, c := range cands {
		if c.Key == first {
			out = append(out, c)
		}
	}
	for _, c := range cands {
		if c.Key != first {
			out = append(out, c)
		}
	}
	return out
}

func (d *RaceDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if addr == "" {
		return nil, ErrEmptyAddress
	}
	if len(d.Candidates) == 0 {
		return nil, errors.New("dialer: no route configured")
	}
	stagger := d.Stagger
	if stagger <= 0 {
		stagger = defaultRaceStagger
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = defaultRaceTimeout
	}
	set := raceSetKey(d.Candidates)
	order := orderCandidates(d.Candidates, recalledWinner(set, time.Now()))

	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		cand RaceCandidate
		conn net.Conn
		err  error
	}
	results := make(chan result, len(order))
	attempt := func(c RaceCandidate) {
		actx, acancel := context.WithTimeout(raceCtx, timeout)
		defer acancel()
		conn, err := c.Dialer.Dial(actx, addr)
		if err == nil && d.ExpectSSHBanner {
			conn, err = awaitSSHBanner(actx, conn)
		}
		results <- result{cand: c, conn: conn, err: err}
	}

	started, finished := 0, 0
	start := func() {
		go attempt(order[started])
		started++
	}
	start()
	timer := time.NewTimer(stagger)
	defer timer.Stop()

	var errs []string
	for finished < started {
		select {
		case r := <-results:
			finished++
			if r.err == nil {
				cancel()
				// close connections from slower attempts that still succeed
				go func(pending int) {
					for i := 0; i < pending; i++ {
						if late := <-results; late.conn != nil {
							_ = late.conn.Close()
						}
					}
				}(started - finished)
				d.mu.Lock()
				d.winner = r.cand
				d.mu.Unlock()
				rememberWinner(set, r.cand.Key, time.Now())
				return r.conn, nil
			}
			errs = append(errs, fmt.Sprintf("%s: %v", r.cand.Name, r.err))
			if started < len(order) {
				start() // failed early: no reason to wait for the stagger
				timer.Reset(stagger)
			}
		case <-timer.C:
			if started < len(order) {
				start()
				timer.Reset(stagger)
			}
		case <-ctx.Done():
			go func(pending int) {
				for i := 0; i < pending; i++ {
					if late := <-results; late.conn != nil {
						_ = late.conn.Close()
					}
				}
			}(started - finished)
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("dialer: all routes failed (%s)", strings.Join(errs, "; "))
}

// awaitSSHBanner reads until the server's "SSH-" identification line (RFC 4253 allows other
// lines before it) and returns a conn that replays everything read.
func awaitSSHBanner(ctx context.Context, conn net.Conn) (net.Conn, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Unix(1, 0)) })
	defer stop()

	br := bufio.NewReaderSize(conn, 4096)
	var seen bytes.Buffer
	for seen.Len() < maxBannerBytes {
		line, err := br.ReadString('\n')
		seen.WriteString(line)
		if strings.HasPrefix(line, "SSH-") {
			_ = conn.SetReadDeadline(time.Time{})
			return &bufferedConn{Conn: conn, r: io.MultiReader(bytes.NewReader(seen.Bytes()), br)}, nil
		}
		if err != nil {
			_ = conn.Close()
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("no SSH reply: %w", ctxErr)
			}
			return nil, fmt.Errorf("no SSH reply: %w", err)
		}
	}
	_ = conn.Close()
	return nil, errors.New("no SSH identification line from server")
}
