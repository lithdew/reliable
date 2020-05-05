package main

import (
	"github.com/valyala/bytebufferpool"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type Endpoint struct {
	mu sync.Mutex
	wg sync.WaitGroup

	conn  net.PacketConn
	conns map[string]*Conn

	closing uint32

	pool *bytebufferpool.Pool
}

func NewEndpoint(conn net.PacketConn) *Endpoint {
	return &Endpoint{conn: conn, conns: make(map[string]*Conn), pool: new(bytebufferpool.Pool)}
}

func (e *Endpoint) getConn(addr net.Addr) *Conn {
	id := addr.String()

	e.mu.Lock()
	defer e.mu.Unlock()

	conn := e.conns[id]
	if conn == nil {
		if atomic.LoadUint32(&e.closing) == 1 {
			return nil
		}

		conn = NewConn(e.conn, addr, e.pool)

		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			check(conn.Run())
		}()

		e.conns[id] = conn
	}

	return conn
}

func (e *Endpoint) clearConn(addr net.Addr) {
	id := addr.String()

	e.mu.Lock()
	conn := e.conns[id]
	delete(e.conns, id)
	e.mu.Unlock()

	conn.Close()
}

func (e *Endpoint) clearConns() {
	e.mu.Lock()
	conns := make([]*Conn, 0, len(e.conns))
	for id, conn := range e.conns {
		conns = append(conns, conn)
		delete(e.conns, id)
	}
	e.mu.Unlock()

	for _, conn := range conns {
		conn.Close()
	}
}

func (e *Endpoint) Addr() net.Addr {
	return e.conn.LocalAddr()
}

func (e *Endpoint) WriteTo(buf []byte, addr net.Addr) error {
	conn := e.getConn(addr)
	if conn == nil {
		return io.EOF
	}
	return conn.Write(buf)
}

func (e *Endpoint) Listen() {
	e.mu.Lock()
	e.wg.Add(1)
	e.mu.Unlock()

	defer e.wg.Done()

	var (
		n    int
		addr net.Addr
		err  error
	)

	buf := make([]byte, math.MaxUint16+1)
	for {
		err = e.conn.SetDeadline(time.Now().Add(1 * time.Second))
		if err != nil {
			break
		}

		n, addr, err = e.conn.ReadFrom(buf)
		if err != nil {
			break
		}

		conn := e.getConn(addr)
		if conn == nil {
			break
		}

		err = conn.Read(buf[:n])
		if err != nil {
			e.clearConn(addr)
		}
	}

	e.clearConns()
}

func (e *Endpoint) Close() error {
	atomic.StoreUint32(&e.closing, 1)
	e.wg.Wait()
	return nil
}
