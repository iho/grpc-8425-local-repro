package transport

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/resolver"
)

// HTTP/2 empty SETTINGS frame (server connection preface).
var http2ServerPreface = []byte{
	0x00, 0x00, 0x00,
	0x04,
	0x00,
	0x00, 0x00, 0x00, 0x00,
}

// hangReadConn serves SETTINGS once, then Read blocks forever.
// Close does not unblock Read; SetReadDeadline does.
type hangReadConn struct {
	local, remote net.Addr
	mu            sync.Mutex
	preface       *bytes.Reader
	rdeadline     time.Time
	deadlineCh    chan struct{} // closed/replaced when deadline changes
}

func newHangReadConn() *hangReadConn {
	return &hangReadConn{
		local:      &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000},
		remote:     &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40001},
		preface:    bytes.NewReader(http2ServerPreface),
		deadlineCh: make(chan struct{}),
	}
}

func (c *hangReadConn) Read(b []byte) (int, error) {
	if c.preface.Len() > 0 {
		n, err := c.preface.Read(b)
		// After preface exhausted, subsequent reads hang.
		return n, err
	}
	for {
		c.mu.Lock()
		d := c.rdeadline
		ch := c.deadlineCh
		c.mu.Unlock()

		if !d.IsZero() {
			delay := time.Until(d)
			if delay <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			t := time.NewTimer(delay)
			select {
			case <-t.C:
				return 0, os.ErrDeadlineExceeded
			case <-ch:
				t.Stop()
				// deadline updated; re-loop
			}
			continue
		}

		// No deadline: wait for one to be set.
		select {
		case <-ch:
		case <-time.After(30 * time.Millisecond):
		}
	}
}

func (c *hangReadConn) Write(b []byte) (int, error) { return len(b), nil }
func (c *hangReadConn) Close() error                 { return nil }
func (c *hangReadConn) LocalAddr() net.Addr          { return c.local }
func (c *hangReadConn) RemoteAddr() net.Addr         { return c.remote }
func (c *hangReadConn) SetDeadline(t time.Time) error {
	_ = c.SetWriteDeadline(t)
	return c.SetReadDeadline(t)
}
func (c *hangReadConn) SetWriteDeadline(time.Time) error { return nil }
func (c *hangReadConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.rdeadline = t
	// wake readers
	old := c.deadlineCh
	c.deadlineCh = make(chan struct{})
	close(old)
	c.mu.Unlock()
	return nil
}

// TestMutePeerCloseLatency is a local demo for grpc-go#8425 / #8534.
//
//	v1.72.x: Close blocks (Fail — hang)
//	v1.77+:  Close returns in ~1s (Pass)
func TestMutePeerCloseLatency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer connectCancel()

	tr, err := NewHTTP2Client(
		connectCtx,
		ctx,
		resolver.Address{Addr: "127.0.0.1:443"},
		ConnectOptions{
			Dialer: func(ctx context.Context, addr string) (net.Conn, error) {
				return newHangReadConn(), nil
			},
			BufferPool: mem.DefaultBufferPool(),
		},
		func(GoAwayReason) {},
	)
	if err != nil {
		t.Fatalf("NewHTTP2Client: %v", err)
	}

	const maxWait = 8 * time.Second
	start := time.Now()
	done := make(chan struct{})
	go func() {
		tr.Close(fmt.Errorf("repro close"))
		close(done)
	}()

	select {
	case <-done:
		elapsed := time.Since(start)
		t.Logf("transport.Close returned in %v", elapsed.Round(time.Millisecond))
		fmt.Fprintf(os.Stderr, "RESULT: Close returned in %v\n", elapsed.Round(time.Millisecond))
		if elapsed >= 700*time.Millisecond && elapsed <= 3*time.Second {
			fmt.Fprintf(os.Stderr, "=> FIXED pattern (~1s SetReadDeadline)\n")
		}
	case <-time.After(maxWait):
		fmt.Fprintf(os.Stderr, "RESULT: Close BLOCKED > %v (VULNERABLE pattern)\n", maxWait)
		t.Fatalf("Close blocked for more than %v (grpc-go#8425 vulnerable behavior)", maxWait)
	}
}
