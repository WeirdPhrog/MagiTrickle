package dnsMITMProxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func freeTCPAddr(t *testing.T) *net.TCPAddr {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr)
}

// startUpstream runs a DNS server over TCP that answers every query with 192.0.2.1.
func startUpstream(t *testing.T) *net.TCPAddr {
	t.Helper()
	return startUpstreamClosingAfter(t, 0)
}

// startUpstreamClosingAfter closes each connection after maxQueries, like a real
// server closing idle ones. Zero keeps the server's default limit.
func startUpstreamClosingAfter(t *testing.T, maxQueries int) *net.TCPAddr {
	t.Helper()
	addr := freeTCPAddr(t)
	started := make(chan struct{})
	srv := &dns.Server{Addr: addr.String(), Net: "tcp", MaxTCPQueries: maxQueries, NotifyStartedFunc: func() { close(started) },
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
			_ = w.WriteMsg(answer(r))
		})}
	go func() { _ = srv.ListenAndServe() }()
	<-started
	t.Cleanup(func() { _ = srv.Shutdown() })
	return addr
}

func answer(r *dns.Msg) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
		A:   net.ParseIP("192.0.2.1"),
	}}
	return m
}

// startScriptedUpstream hands the n-th accepted connection to serve and counts
// connections.
func startScriptedUpstream(t *testing.T, serve func(n int32, conn net.Conn)) (*net.TCPAddr, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := new(atomic.Int32)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serve(accepted.Add(1), conn)
		}
	}()
	return listener.Addr().(*net.TCPAddr), accepted
}

func queryOnce(t *testing.T, proxy *net.TCPAddr) error {
	t.Helper()
	conn, err := dns.DialTimeout("tcp", proxy.String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	m := new(dns.Msg)
	m.SetQuestion("once.example.", dns.TypeA)
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMsg(m); err != nil {
		return err
	}
	_, err = conn.ReadMsg()
	return err
}

func startProxy(t *testing.T, upstream *net.TCPAddr, maxConcurrent uint, timeout time.Duration) *net.TCPAddr {
	t.Helper()
	p := NewDNSMITMProxy(upstream.String(), 2, maxConcurrent, timeout)
	t.Cleanup(func() { _ = p.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	addr := freeTCPAddr(t)
	go func() { _ = p.ListenTCP(ctx, addr) }()
	for range 50 {
		if c, err := net.Dial("tcp", addr.String()); err == nil {
			_ = c.Close()
			return addr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("proxy did not start")
	return nil
}

func exchangeOn(t *testing.T, conn *dns.Conn, name string) {
	t.Helper()
	m := new(dns.Msg)
	m.SetQuestion(name, dns.TypeA)
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if err := conn.WriteMsg(m); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := conn.ReadMsg()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(r.Answer) != 1 {
		t.Fatalf("answer: %v", r.Answer)
	}
}

// A connection that waits for its next query must not occupy a request slot:
// with a single slot, a second client still gets served while the first one
// sits idle on an open connection.
func TestIdleTCPConnectionDoesNotHoldARequestSlot(t *testing.T) {
	proxy := startProxy(t, startUpstream(t), 1, 3*time.Second)

	idle, err := dns.DialTimeout("tcp", proxy.String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	exchangeOn(t, idle, "first.example.") // the connection stays open and idle afterwards

	other, err := dns.DialTimeout("tcp", proxy.String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	exchangeOn(t, other, "second.example.")

	// The idle connection is still usable: several queries per connection are served.
	exchangeOn(t, idle, "third.example.")
}

// An upstream that closes its connections must not cost a client its query,
// whether the client reuses its own connection or opens new ones.
func TestClosedUpstreamConnectionIsReplaced(t *testing.T) {
	proxy := startProxy(t, startUpstreamClosingAfter(t, 1), 4, 2*time.Second)

	client, err := dns.DialTimeout("tcp", proxy.String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for range 20 {
		exchangeOn(t, client, "kept.example.")
	}
	for i := range 20 {
		if err := queryOnce(t, proxy); err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
	}
}

// One attempt on a new connection: a server that keeps failing gets no more.
func TestClosedUpstreamConnectionIsRetriedOnce(t *testing.T) {
	upstream, accepted := startScriptedUpstream(t, func(n int32, conn net.Conn) {
		defer conn.Close()
		if n > 1 {
			return
		}
		dnsConn := &dns.Conn{Conn: conn}
		if query, err := dnsConn.ReadMsg(); err == nil {
			_ = dnsConn.WriteMsg(answer(query))
		}
	})
	proxy := startProxy(t, upstream, 4, 2*time.Second)

	if err := queryOnce(t, proxy); err != nil {
		t.Fatal(err)
	}
	if err := queryOnce(t, proxy); err == nil {
		t.Fatal("no server answered, yet the query succeeded")
	}
	if got := accepted.Load(); got != 2 {
		t.Fatalf("upstream connections = %d, want the first and one retry", got)
	}
}

// A connection that was never reused has no excuse: the failure is real.
func TestNewUpstreamConnectionIsNotRetried(t *testing.T) {
	upstream, accepted := startScriptedUpstream(t, func(_ int32, conn net.Conn) { _ = conn.Close() })
	proxy := startProxy(t, upstream, 4, 2*time.Second)

	if err := queryOnce(t, proxy); err == nil {
		t.Fatal("no server answered, yet the query succeeded")
	}
	if got := accepted.Load(); got != 1 {
		t.Fatalf("upstream connections = %d, want 1", got)
	}
}

// A reply that fails validation is suspicious, so it must not be asked for again.
func TestUnacceptableUpstreamResponseIsNotRetried(t *testing.T) {
	upstream, accepted := startScriptedUpstream(t, func(n int32, conn net.Conn) {
		defer conn.Close()
		dnsConn := &dns.Conn{Conn: conn}
		for served := 0; ; served++ {
			query, err := dnsConn.ReadMsg()
			if err != nil {
				return
			}
			reply := answer(query)
			if served == 1 {
				reply.Id++
			}
			_ = dnsConn.WriteMsg(reply)
		}
	})
	proxy := startProxy(t, upstream, 4, 2*time.Second)

	if err := queryOnce(t, proxy); err != nil {
		t.Fatal(err)
	}
	if err := queryOnce(t, proxy); err == nil {
		t.Fatal("a reply with the wrong ID was accepted")
	}
	if got := accepted.Load(); got != 1 {
		t.Fatalf("upstream connections = %d, want 1", got)
	}
}

func TestConnectionClosed(t *testing.T) {
	timeout := &net.OpError{Op: "read", Err: timeoutError{}}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"closed by the server", fmt.Errorf("failed to read length: %w", io.EOF), true},
		{"reset", fmt.Errorf("failed to read length: %w", &net.OpError{Op: "read", Err: syscall.ECONNRESET}), true},
		{"broken pipe", fmt.Errorf("failed to write request: %w", &net.OpError{Op: "write", Err: syscall.EPIPE}), true},
		{"timeout", fmt.Errorf("failed to read length: %w", timeout), false},
		{"cut off inside a message", fmt.Errorf("failed to read response: %w", io.ErrUnexpectedEOF), false},
		{"unacceptable response", errors.New("unexpected DNS response header, transaction ID or question"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := connectionClosed(tc.err); got != tc.want {
				t.Errorf("connectionClosed(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string { return "i/o timeout" }
func (timeoutError) Timeout() bool { return true }
