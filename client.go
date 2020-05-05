package reliable

import (
	"errors"
	"fmt"
	"github.com/lithdew/seq"
	"github.com/valyala/bytebufferpool"
	"io"
	"net"
	"time"
)

var ErrDuplicatePacket = errors.New("got a duplicate packet")

type Client struct {
	writeBufferSize uint16
	readBufferSize  uint16

	conn net.PacketConn
	addr net.Addr

	w *seq.Buffer
	r *seq.Buffer

	exit chan struct{}
	wq   chan *workItem
	rq   chan *workItem

	pool *bytebufferpool.Pool

	oldestSentAck uint16
	oldestUnacked uint16

	handler Handler
}

func NewClient(conn net.PacketConn, addr net.Addr, opts ...Option) *Client {
	c := &Client{
		conn: conn,
		addr: addr,

		exit: make(chan struct{}),
	}

	for _, opt := range opts {
		opt.applyClient(c)
	}

	if c.writeBufferSize == 0 {
		c.writeBufferSize = DefaultWriteBufferSize
	}

	if c.readBufferSize == 0 {
		c.readBufferSize = DefaultReadBufferSize
	}

	c.w = seq.NewBuffer(c.writeBufferSize)
	c.r = seq.NewBuffer(c.readBufferSize)

	c.wq = make(chan *workItem, 16)
	c.rq = make(chan *workItem, 16)

	if c.pool == nil {
		c.pool = new(bytebufferpool.Pool)
	}

	return c
}

func (c *Client) Write(buf []byte) error {
	w := acquireWorkItem()
	defer releaseWorkItem(w)

	w.buf = buf

	select {
	case <-c.exit:
		return io.EOF
	case c.wq <- w:
	}

	select {
	case <-c.exit:
		return io.EOF
	case err := <-w.done:
		return err
	}
}

func (c *Client) Receive(buf []byte) error {
	w := acquireWorkItem()
	defer releaseWorkItem(w)

	w.buf = buf

	select {
	case <-c.exit:
		return io.EOF
	case c.rq <- w:
	}

	select {
	case <-c.exit:
		return io.EOF
	case err := <-w.done:
		return err
	}
}

func (c *Client) Close() {
	close(c.exit)
}

func (c *Client) Start() error {
	wq, rq := c.wq, c.rq

	updateTicker := time.NewTicker(100 * time.Millisecond)
	defer updateTicker.Stop()

	for {
		select {
		case <-c.exit:
			return nil
		case now := <-updateTicker.C:
			if err := c.retransmitStalePackets(now); err != nil {
				return fmt.Errorf("error occurred retransmitting stale packets: %w", err)
			}
		case w := <-wq:
			next := c.w.Next()
			ack, ackBits := c.r.GenerateLatestBitset32()

			c.updateOldestSentAck(ack)

			h := PacketHeader{
				seq:     next,
				ack:     ack,
				ackBits: ackBits,
			}

			buf := c.pool.Get()
			buf.B = h.AppendTo(buf.B)
			buf.B = append(buf.B, w.buf...)

			if !h.unordered {
				c.tryRecycleSentPacket(h.seq)

				c.w.Insert(h.seq, &SentPacket{
					time: time.Now(),
					buf:  buf,
				})
			}

			//fmt.Printf(
			//	"%s: [sent packet] (seq=%d) (ack=%d) (ackBits=%032b) (oldest unacked=%d)\n",
			//	c.addr, h.seq, h.ack, h.ackBits, c.oldestUnacked,
			//)

			err := c.transmit(buf.B)
			if err != nil {
				err = fmt.Errorf("failed to write packet: %w", err)
			}

			w.finish(err)

			if err != nil {
				return err
			}

			// FIXME(kenta): If the next packet to be written exceeds the write sequence buffer, then stop
			//  future writes.

			//if c.w.Next() == c.oldestUnacked+c.r.Len() {
			//	wq = nil
			//
			//	fmt.Printf(
			//		"%s: write queue is blocked after sending %d (next=%d) (oldest unacked=%d)\n",
			//		c.addr, next, c.w.Next(), c.oldestUnacked,
			//	)
			//}
		case w := <-rq:
			h, buf, err := UnmarshalPacketHeader(w.buf)
			if err != nil {
				err = fmt.Errorf("failed to read packet header: %w", err)
			}

			if !h.unordered {
				if err == nil {
					if c.r.Exists(h.seq) {
						err = fmt.Errorf("sequence number %d: %w", h.seq, ErrDuplicatePacket)
					}
				}
				if err == nil {
					ok := c.r.Insert(h.seq, &RecvPacket{
						time: time.Now(),
						size: len(buf),
					})

					if !ok {
						err = fmt.Errorf("packet received w/ sequence number %d is outdated", h.seq)
					}
				}
			}

			if err == nil && c.handler != nil {
				c.handler(c.addr, h.seq, buf)
			}

			if errors.Is(err, ErrDuplicatePacket) {
				err = nil
			}

			w.finish(err)

			if err != nil {
				return err
			}

			//fmt.Printf(
			//	"%s: [recv packet] (seq=%d) (ack=%d) (ackBits=%032b) (oldest unacked=%d) (oldest sent ack=%d)\n",
			//	c.addr, h.seq, h.ack, h.ackBits, c.oldestUnacked, c.oldestSentAck,
			//)

			if err := c.sendAcksIfNecessary(); err != nil {
				return err
			}

			c.processAcks(h.ack, h.ackBits)

			c.updateOldestUnacked()

			// FIXME(kenta)

			//if next := c.w.Next(); wq == nil && seq.LT(next, c.oldestUnacked+c.r.Len()) {
			//	wq = c.wq
			//
			//	fmt.Printf(
			//		"%s: write queue is unblocked at %d while receiving (seq=%d) (oldest unacked=%d)\n",
			//		c.addr, next, h.seq, c.oldestUnacked,
			//	)
			//}
		}
	}
}

func (c *Client) transmit(buf []byte) error {
	n, err := c.conn.WriteTo(buf, c.addr)
	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		if netErr.Err.Error() == "use of closed network connection" {
			err = fmt.Errorf("%s: %w", err, io.EOF)
		}
	}
	return err
}

func (c *Client) retransmitStalePackets(now time.Time) error {
	for i := uint16(0); i < c.w.Len(); i++ {
		sent, ok := c.w.Find(c.oldestUnacked + i).(*SentPacket)
		if !ok || sent.acked {
			continue
		}

		if now.Sub(sent.time) < 100*time.Millisecond {
			continue
		}

		if err := c.transmit(sent.buf.B); err != nil {
			return err
		}

		sent.time = now
	}

	return nil
}

func (c *Client) sendAcksIfNecessary() error {
	for {
		// See if the next 32 packets have been received, which would be a condition where we need
		// to send an ACK before the read buffer rolls over.

		for i := uint16(0); i < AckBitsetSize; i++ {
			if !c.r.Exists(c.oldestSentAck + i) {
				return nil
			}
		}

		c.oldestSentAck += AckBitsetSize - 1

		// Construct the ACK packet.

		next := c.w.Next()
		ack, ackBits := c.oldestSentAck, c.r.GenerateBitset32(c.oldestSentAck)

		h := PacketHeader{
			seq:     next,
			ack:     ack,
			ackBits: ackBits,
		}

		//fmt.Printf(
		//	"%s: [sending ack] (seq=%d) (ack=%d) (ackBits=%032b)\n",
		//	c.addr, h.seq, h.ack, h.ackBits,
		//)

		buf := c.pool.Get()
		buf.B = h.AppendTo(buf.B)

		if !h.unordered {
			c.tryRecycleSentPacket(h.seq)

			c.w.Insert(h.seq, &SentPacket{
				time: time.Now(),
				buf:  buf,
			})
		}

		// Write the ACK packet.

		if err := c.transmit(buf.B); err != nil {
			return fmt.Errorf("failed to write ack packet: %w", err)
		}
	}
}

func (c *Client) processAcks(ack uint16, ackBits uint32) {
	for i := uint16(0); i < AckBitsetSize; i, ackBits = i+1, ackBits>>1 {
		if ackBits&1 == 0 {
			continue
		}

		num := ack - i

		packet, ok := c.w.Find(num).(*SentPacket)
		if !ok || packet.acked {
			continue
		}

		c.pool.Put(packet.buf)

		packet.acked = true
		packet.buf = nil
	}
}

func (c *Client) tryRecycleSentPacket(seq uint16) {
	packet, ok := c.w.At(seq).(*SentPacket)
	if !ok || packet.buf == nil {
		return
	}

	c.pool.Put(packet.buf)

	packet.acked = false
	packet.buf = nil
}

func (c *Client) updateOldestSentAck(ack uint16) {
	oldestSentAck := c.oldestSentAck

	for oldestSentAck <= ack {
		if !c.r.Exists(oldestSentAck) {
			break
		}
		oldestSentAck++
	}

	if c.oldestSentAck == oldestSentAck {
		return
	}

	//fmt.Printf("%s: [oldest sent ack] before=%d after=%d\n", c.addr, c.oldestSentAck, oldestSentAck)

	c.oldestSentAck = oldestSentAck
}

func (c *Client) updateOldestUnacked() {
	oldestUnacked := c.oldestUnacked

	for {
		packet, ok := c.w.Find(oldestUnacked + 1).(*SentPacket)
		if !ok || !packet.acked {
			break
		}

		oldestUnacked++
	}

	if c.oldestUnacked == oldestUnacked {
		return
	}

	//fmt.Printf("%s: [oldest unacked] (before=%d) (after=%d)\n", c.addr, c.oldestUnacked, oldestUnacked)

	c.oldestUnacked = oldestUnacked
}
