package dnsMITMProxy

import (
	"context"
	"errors"
	"net"
	"sync"
)

// connPool is a simple connection pool based on a channel
type connPool struct {
	network string
	addr    string
	pool    chan net.Conn
	maxIdle uint
	mu      sync.Mutex
	closed  bool
}

func newConnPool(network, addr string, maxIdle uint) *connPool {
	return &connPool{
		network: network,
		addr:    addr,
		pool:    make(chan net.Conn, maxIdle),
		maxIdle: maxIdle,
	}
}

// Get returns an idle connection or dials a new one, and reports whether the
// connection was reused: the other side may have closed a reused one meanwhile.
func (p *connPool) Get(ctx context.Context) (net.Conn, bool, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, false, errors.New("pool is closed")
	}
	// Check context before trying to get/create connection
	if ctx.Err() != nil {
		p.mu.Unlock()
		return nil, false, ctx.Err()
	}

	select {
	case conn := <-p.pool:
		p.mu.Unlock()
		return conn, true, nil
	default:
		p.mu.Unlock()
		conn, err := p.Dial(ctx)
		return conn, false, err
	}
}

// Dial opens a new connection, bypassing the idle ones.
func (p *connPool) Dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, p.network, p.addr)
}

func (p *connPool) Put(conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = conn.Close()
		return
	}
	select {
	case p.pool <- conn:
	default:
		_ = conn.Close()
	}
}

func (p *connPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true

	close(p.pool)
	for conn := range p.pool {
		_ = conn.Close()
	}
}
