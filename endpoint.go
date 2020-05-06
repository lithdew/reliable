package reliable

import (
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type PacketHandler func(addr net.Addr, seq uint16, buf []byte)
type ErrorHandler func(addr net.Addr, err error)

type Endpoint struct {
	writeBufferSize uint16 // write buffer size that must be a divisor of 65536
	readBufferSize  uint16 // read buffer size that must be a divisor of 65536

	updatePeriod time.Duration // how often time-dependant parts of the protocol get checked
	ackTimeout   time.Duration // how long we wait until unacked packets should be resent

	mu sync.Mutex
	wg sync.WaitGroup

	pool *Pool

	ph PacketHandler
	eh ErrorHandler

	addr  net.Addr
	conn  net.PacketConn
	conns map[string]*Conn

	closing uint32
}

func NewEndpoint(conn net.PacketConn, opts ...EndpointOption) *Endpoint {
	e := &Endpoint{conn: conn, addr: conn.LocalAddr(), conns: make(map[string]*Conn)}

	for _, opt := range opts {
		opt.applyEndpoint(e)
	}

	if e.writeBufferSize == 0 {
		e.writeBufferSize = DefaultWriteBufferSize
	}

	if e.readBufferSize == 0 {
		e.readBufferSize = DefaultReadBufferSize
	}

	if e.ackTimeout == 0 {
		e.ackTimeout = DefaultACKTimeout
	}

	if e.updatePeriod == 0 {
		e.updatePeriod = DefaultUpdatePeriod
	}

	if e.pool == nil {
		e.pool = new(Pool)
	}

	return e
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

		conn = NewConn(
			e.conn,
			addr,
			WithWriteBufferSize(e.writeBufferSize),
			WithReadBufferSize(e.readBufferSize),
			WithUpdatePeriod(e.updatePeriod),
			WithACKTimeout(e.ackTimeout),
			WithBufferPool(e.pool),
			WithPacketHandler(e.ph),
			WithErrorHandler(e.eh),
		)

		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			conn.Run()
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
	return e.addr
}

func (e *Endpoint) WriteReliablePacket(buf []byte, addr net.Addr) error {
	conn := e.getConn(addr)
	if conn == nil {
		return io.EOF
	}
	return conn.WriteReliablePacket(buf)
}

func (e *Endpoint) WriteUnreliablePacket(buf []byte, addr net.Addr) error {
	conn := e.getConn(addr)
	if conn == nil {
		return io.EOF
	}
	return conn.WriteUnreliablePacket(buf)
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
		n, addr, err = e.conn.ReadFrom(buf)
		if err != nil {
			break
		}

		conn := e.getConn(addr)
		if conn == nil {
			break
		}

		header, buf, err := UnmarshalPacketHeader(buf[:n])
		if err == nil {
			err = conn.Read(header, buf)
		}
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
