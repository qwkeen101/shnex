package dialer

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// pipeDialer returns one end of a pipe whose far end writes banner after delay.
func pipeDialer(delay time.Duration, banner string) Dialer {
	return DialFunc(func(ctx context.Context, addr string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			time.Sleep(delay)
			_, _ = server.Write([]byte(banner))
			_, _ = io.Copy(io.Discard, server)
		}()
		return client, nil
	})
}

func failDialer(err error) Dialer {
	return DialFunc(func(ctx context.Context, addr string) (net.Conn, error) { return nil, err })
}

func hangDialer() Dialer {
	return DialFunc(func(ctx context.Context, addr string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
}

func resetRaceMemory() {
	raceMemory.Lock()
	raceMemory.m = map[string]raceMemo{}
	raceMemory.Unlock()
}

func readLine(t *testing.T, c net.Conn) string {
	t.Helper()
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(buf[:n])
}

func TestRacePrimaryWins(t *testing.T) {
	resetRaceMemory()
	d := NewRaceDialer([]RaceCandidate{
		{Key: "a", Name: "A", Dialer: pipeDialer(0, "SSH-2.0-A\r\n")},
		{Key: "b", Name: "B", Dialer: pipeDialer(0, "SSH-2.0-B\r\n")},
	}, true)
	c, err := d.Dial(context.Background(), "h:22")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := readLine(t, c); got != "SSH-2.0-A\r\n" {
		t.Fatalf("banner must be replayed, got %q", got)
	}
	if w, fb, _ := d.Winner(); w.Key != "a" || fb {
		t.Fatalf("winner %+v fallback %v", w, fb)
	}
}

func TestRaceStaggerStartsBackupWhenPrimaryHangs(t *testing.T) {
	resetRaceMemory()
	d := NewRaceDialer([]RaceCandidate{
		{Key: "a", Name: "A", Dialer: hangDialer()},
		{Key: "b", Name: "B", Dialer: pipeDialer(0, "SSH-2.0-B\r\n")},
	}, true)
	d.Stagger = 50 * time.Millisecond
	start := time.Now()
	c, err := d.Dial(context.Background(), "h:22")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if time.Since(start) > 2*time.Second {
		t.Fatal("backup should start after the stagger, not after the primary times out")
	}
	if w, fb, _ := d.Winner(); w.Key != "b" || !fb {
		t.Fatalf("want fallback to b, got %+v %v", w, fb)
	}
}

func TestRaceSilentPrimaryCountsAsDead(t *testing.T) {
	// xray-like: Dial succeeds but nothing ever arrives
	resetRaceMemory()
	d := NewRaceDialer([]RaceCandidate{
		{Key: "a", Name: "A", Dialer: pipeDialer(time.Hour, "")},
		{Key: "b", Name: "B", Dialer: pipeDialer(0, "SSH-2.0-B\r\n")},
	}, true)
	d.Stagger = 50 * time.Millisecond
	c, err := d.Dial(context.Background(), "h:22")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if w, _, _ := d.Winner(); w.Key != "b" {
		t.Fatalf("want b, got %+v", w)
	}
}

func TestRaceEarlyFailureSkipsStagger(t *testing.T) {
	resetRaceMemory()
	d := NewRaceDialer([]RaceCandidate{
		{Key: "a", Name: "A", Dialer: failDialer(errors.New("refused"))},
		{Key: "b", Name: "B", Dialer: pipeDialer(0, "SSH-2.0-B\r\n")},
	}, true)
	d.Stagger = time.Hour
	c, err := d.Dial(context.Background(), "h:22")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestRaceAllFail(t *testing.T) {
	resetRaceMemory()
	d := NewRaceDialer([]RaceCandidate{
		{Key: "a", Name: "A", Dialer: failDialer(errors.New("refused"))},
		{Key: "b", Name: "B", Dialer: failDialer(errors.New("timeout"))},
	}, true)
	_, err := d.Dial(context.Background(), "h:22")
	if err == nil || !strings.Contains(err.Error(), "A: refused") || !strings.Contains(err.Error(), "B: timeout") {
		t.Fatalf("error should name every route: %v", err)
	}
}

func TestRaceBannerAfterPreambleLines(t *testing.T) {
	resetRaceMemory()
	d := NewRaceDialer([]RaceCandidate{
		{Key: "a", Name: "A", Dialer: pipeDialer(0, "welcome\r\nSSH-2.0-A\r\nKEX")},
	}, true)
	c, err := d.Dial(context.Background(), "h:22")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, _ := io.ReadAll(io.LimitReader(c, int64(len("welcome\r\nSSH-2.0-A\r\nKEX"))))
	if string(got) != "welcome\r\nSSH-2.0-A\r\nKEX" {
		t.Fatalf("all bytes must be replayed in order, got %q", got)
	}
}

func TestRaceRemembersWinner(t *testing.T) {
	resetRaceMemory()
	cands := []RaceCandidate{
		{Key: "a", Name: "A", Dialer: failDialer(errors.New("dead"))},
		{Key: "b", Name: "B", Dialer: pipeDialer(0, "SSH-2.0-B\r\n")},
	}
	d := NewRaceDialer(cands, true)
	c, err := d.Dial(context.Background(), "h:22")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if got := orderCandidates(cands, recalledWinner(raceSetKey(cands), time.Now())); got[0].Key != "b" {
		t.Fatalf("next dial should try the last winner first, got %s", got[0].Key)
	}
	if recalledWinner(raceSetKey(cands), time.Now().Add(raceWinnerMemoryTTL+time.Second)) != "" {
		t.Fatal("memory must expire")
	}
}

func TestRaceWithoutBannerForNonSSH(t *testing.T) {
	resetRaceMemory()
	d := NewRaceDialer([]RaceCandidate{{Key: "a", Name: "A", Dialer: pipeDialer(time.Hour, "")}}, false)
	c, err := d.Dial(context.Background(), "h:23")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}
