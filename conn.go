package reliable

import (
	"fmt"
	"io"
	"net"
)

type transmitFunc func(buf []byte) (bool, error)

type Conn struct {
	addr     net.Addr
	conn     net.PacketConn
	protocol *Protocol
}

func NewConn(addr net.Addr, conn net.PacketConn, opts ...ProtocolOption) *Conn {
	p := NewProtocol(opts...)
	return &Conn{addr: addr, conn: conn, protocol: p}
}

func (c *Conn) WriteReliablePacket(buf []byte) error {
	buf, err := c.protocol.WritePacket(true, buf)
	if err != nil {
		return err
	}

	_, err = c.transmit(buf)
	return err
}

func (c *Conn) WriteUnreliablePacket(buf []byte) error {
	buf, err := c.protocol.WritePacket(false, buf)
	if err != nil {
		return err
	}

	_, err = c.transmit(buf)
	return err
}

func (c *Conn) Read(header PacketHeader, buf []byte) error {
	buf = c.protocol.ReadPacket(header, buf)

	if len(buf) != 0 {
		_, err := c.transmit(buf)
		return err
	}

	return nil
}

func (c *Conn) Close() {
	c.protocol.Close()
}

func (c *Conn) Run() {
	c.protocol.Run(c.transmit)
}

func (c *Conn) transmit(buf []byte) (EOF bool, err error) {
	n, err := c.conn.WriteTo(buf, c.addr)

	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}

	EOF = isEOF(err)

	if err != nil && !EOF {
		err = fmt.Errorf("failed to transmit packet: %w", err)
		return
	}

	return
}
