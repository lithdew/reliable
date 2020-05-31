package reliable

import (
	"fmt"
	"io"
	"net"
	"time"
)

type transmitFunc func(buf []byte) (bool, error)

type Conn struct {
	addr net.Addr
	conn net.PacketConn

	p *Protocol

	updatePeriod  time.Duration // how often time-dependant parts of the p get checked
	resendTimeout time.Duration // how long we wait until unacked packets should be resent

	exit chan struct{} // signal channel to close the conn
}

func NewConn(addr net.Addr, conn net.PacketConn, opts ...ProtocolOption) *Conn {
	return &Conn{
		addr:          addr,
		conn:          conn,
		p:             NewProtocol(opts...),
		updatePeriod:  DefaultUpdatePeriod,
		resendTimeout: DefaultResendTimeout,
		exit:          make(chan struct{}),
	}
}

func (c *Conn) WriteReliablePacket(buf []byte) error {
	buf, err := c.p.WritePacket(true, buf)
	if err != nil {
		return err
	}

	_, err = c.transmit(buf)
	return err
}

func (c *Conn) WriteUnreliablePacket(buf []byte) error {
	buf, err := c.p.WritePacket(false, buf)
	if err != nil {
		return err
	}

	_, err = c.transmit(buf)
	return err
}

func (c *Conn) Read(header PacketHeader, buf []byte) error {
	needed := c.p.ReadPacket(header, buf)
	if !needed {
		return nil
	}

	return c.writeAcks()
}

func (c *Conn) Close() {
	close(c.exit)
	c.p.Close()
}

func (c *Conn) Run() {
	ticker := time.NewTicker(c.updatePeriod)
	defer ticker.Stop()

	for {
		select {
		case <-c.exit:
			return
		case <-ticker.C:
			if err := c.retransmitUnackedPackets(); err != nil {
				c.p.callErrorHandler(err)
			}
		}
	}
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

func (c *Conn) writeAcks() error {
	c.p.mu.Lock()
	defer c.p.mu.Unlock()

	for {
		needed := c.p.checkIfAck()
		if !needed {
			break
		}

		header := c.p.createAck()

		buf := c.p.write(header, nil)
		if _, err := c.transmit(buf); err != nil {
			return fmt.Errorf("failed to transmit acks: %w", err)
		}
	}

	return nil
}

func (c *Conn) retransmitUnackedPackets() error {
	c.p.mu.Lock()
	defer c.p.mu.Unlock()

	for idx := uint16(0); idx < c.p.writeQueueLen(); idx++ {
		buf, needed := c.p.checkIfRetransmit(idx, c.resendTimeout)
		if !needed {
			continue
		}

		if isEOF, err := c.transmit(buf); err != nil {
			return fmt.Errorf("failed to retransmit unacked packet: %w", err)
		} else if isEOF {
			break
		}

		c.p.incrementWqe(idx)
	}

	return nil
}
