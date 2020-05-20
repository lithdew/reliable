package reliable

import (
	"fmt"
	"io"
	"net"
)

type transmitFunc func(addr net.Addr, buf []byte) (bool, error)

type Conn struct {
	conn     net.PacketConn
	protocol *Protocol
}

func NewConn(conn net.PacketConn, addr net.Addr, opts ...ProtocolOption) *Conn {
	p := NewProtocol(addr, opts...)
	return &Conn{conn: conn, protocol: p}
}

func (c *Conn) WriteReliablePacket(buf []byte) error {
	addr, buf, err := c.protocol.writePacket(true, buf)
	if err != nil {
		return err
	}

	_, err = c.transmit(addr, buf)
	return err
}

func (c *Conn) WriteUnreliablePacket(buf []byte) error {
	addr, buf, err := c.protocol.writePacket(false, buf)
	if err != nil {
		return err
	}

	_, err = c.transmit(addr, buf)
	return err
}

func (c *Conn) Read(header PacketHeader, buf []byte) error {
	addr, buf, err := c.protocol.Read(header, buf)
	if err != nil {
		return err
	}

	if len(buf) != 0 {
		_, err = c.transmit(addr, buf)
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

func (c *Conn) transmit(addr net.Addr, buf []byte) (EOF bool, err error) {
	n, err := c.conn.WriteTo(buf, addr)

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
