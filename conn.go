package reliable

import (
	"net"
	"io"
	// "fmt"
)

type writeFunc func(addr net.Addr, buf []byte) error

type Conn struct {
  conn net.PacketConn
  protocol    *Protocol
}

func NewConn(conn net.PacketConn, addr net.Addr, opts ...ProtocolOption) *Conn {
	p := NewProtocol(addr, opts...)
	return &Conn{conn: conn, protocol: p}
}

func (c *Conn) Run() {
	c.protocol.Run(func(addr net.Addr, buf []byte) error {
		return c.transmit(addr, buf)
	})
}

func (c *Conn) Close() {
	c.protocol.Close()
}

func (c *Conn) Read(header PacketHeader, buf []byte) error {
	return p.protocol.Read(header, buf, func(addr net.Addr, buf []byte) error {
		return c.transmit(addr, buf)
	})
}

func (c *Conn) WriteReliablePacket(buf []byte) error {
	return p.writePacket(true, buf, func(addr net.Addr, buf []byte) error {
		return c.transmit(addr, buf)
	})
}

func (c *Conn) WriteUnreliablePacket(buf []byte) error {
	return p.writePacket(false, buf, func(addr net.Addr, buf []byte) error {
		return c.transmit(addr, buf)
	})
}

func (c *Conn) transmit(addr net.Addr, buf []byte) error {
	n, err := c.conn.WriteTo(buf, addr)
	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}
	// return fmt.Errorf("failed to transmit packet: %w", err)
	return err
}
