package dnsMITMProxy

import (
	"context"
	"net"
	"sync"
	"testing"
)

func TestPoolConcurrentCloseGetPut(t *testing.T) {
	for n := 0; n < 100; n++ {
		p := newConnPool("tcp", "127.0.0.1:0", 1)
		conn, peer := net.Pipe()
		p.Put(conn)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				conn, err := p.Get(context.Background())
				if err == nil {
					if conn == nil {
						t.Error("closed pool returned nil connection without error")
						return
					}
					p.Put(conn)
				}
				p.Close()
			}()
		}
		close(start)
		wg.Wait()
		p.Close()
		peer.Close()
		if conn, err := p.Get(context.Background()); err == nil || conn != nil {
			t.Fatal("closed pool accepted Get")
		}
	}
}
