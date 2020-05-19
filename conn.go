package reliable

import (
	"io"
	"net"
)

type transmitFunc func(addr net.Addr, buf []byte) error

type Conn struct {
	conn     net.PacketConn
	protocol *Protocol
}

func NewConn(conn net.PacketConn, addr net.Addr, opts ...ProtocolOption) *Conn {
	p := NewProtocol(addr, opts...)
	return &Conn{conn: conn, protocol: p}
}

func (c *Conn) WriteReliablePacket(buf []byte) error {
	return c.protocol.writePacket(true, buf, c.transmit)
}

func (c *Conn) WriteUnreliablePacket(buf []byte) error {
	return c.protocol.writePacket(false, buf, c.transmit)
}

func (c *Conn) Read(header PacketHeader, buf []byte) error {
	return c.protocol.Read(header, buf, c.transmit)
}

func (c *Conn) Close() {
	c.protocol.Close()
}

func (c *Conn) Run() {
	c.protocol.Run(c.transmit)
}

func (c *Conn) transmit(addr net.Addr, buf []byte) error {
	n, err := c.conn.WriteTo(buf, addr)
	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}

	return err
}
