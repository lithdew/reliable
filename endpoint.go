package reliable

import (
	"errors"
	"fmt"
	"github.com/valyala/bytebufferpool"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
)

type Handler func(addr net.Addr, seq uint16, buf []byte)

type Endpoint struct {
	writeBufferSize uint16
	readBufferSize  uint16

	mu      sync.Mutex
	wg      sync.WaitGroup
	closing uint32

	conn net.PacketConn
	pool *bytebufferpool.Pool

	clients map[string]*Client
	handler Handler
}

func NewEndpoint(conn net.PacketConn, opts ...Option) *Endpoint {
	endpoint := &Endpoint{
		conn:    conn,
		clients: make(map[string]*Client),
	}

	for _, opt := range opts {
		opt.applyEndpoint(endpoint)
	}

	if endpoint.writeBufferSize == 0 {
		endpoint.writeBufferSize = DefaultWriteBufferSize
	}

	if endpoint.readBufferSize == 0 {
		endpoint.readBufferSize = DefaultReadBufferSize
	}

	if endpoint.pool == nil {
		endpoint.pool = new(bytebufferpool.Pool)
	}

	return endpoint
}

func (e *Endpoint) getClient(addr net.Addr) *Client {
	if atomic.LoadUint32(&e.closing) == 1 {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	client, exists := e.clients[addr.String()]
	if !exists {
		client = NewClient(
			e.conn,
			addr,
			WithWriteBufferSize(e.writeBufferSize),
			WithReadBufferSize(e.readBufferSize),
			WithHandler(e.handler),
			WithBufferPool(e.pool),
		)

		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			client.Start()
		}()

		e.clients[addr.String()] = client
	}
	return client
}

func (e *Endpoint) Addr() net.Addr {
	return e.conn.LocalAddr()
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
		if err == nil {
			err = e.ReceiveFrom(buf[:n], addr)
		}
		if err != nil {
			var netErr *net.OpError
			if errors.As(err, &netErr) {
				if netErr.Err.Error() == "use of closed network connection" {
					err = fmt.Errorf("%s: %w", err, io.EOF)
				}
			}

			if errors.Is(err, io.EOF) {
				break
			}

			continue
		}
	}

	// If Close() was not called, then manually close the connection.

	closing := !atomic.CompareAndSwapUint32(&e.closing, 0, 1)

	if !closing {
		if closeErr := e.conn.Close(); closeErr != nil {
			err = fmt.Errorf("%s: %w", closeErr, err)
		}
	}

	// Cleanup all clients.

	for addr, client := range e.clients {
		delete(e.clients, addr)
		client.Close()
	}
}

func (e *Endpoint) Close() error {
	if !atomic.CompareAndSwapUint32(&e.closing, 0, 1) {
		return nil
	}

	if err := e.conn.Close(); err != nil {
		return fmt.Errorf("failed to close packet conn: %w", err)
	}

	e.mu.Lock()
	e.wg.Wait()
	e.mu.Unlock()

	return nil
}

func (e *Endpoint) WriteTo(buf []byte, addr net.Addr) error {
	client := e.getClient(addr)
	if client == nil {
		return io.EOF
	}
	return client.Write(buf)
}

func (e *Endpoint) ReceiveFrom(buf []byte, addr net.Addr) error {
	client := e.getClient(addr)
	if client == nil {
		return io.EOF
	}
	return client.Receive(buf)
}
