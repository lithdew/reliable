package reliable

import (
	"github.com/lithdew/seq"
	"io"
	"sync"
	"time"
)

type ProtocolPacketHandler func(buf []byte, seq uint16)
type ProtocolErrorHandler func(err error)

type Protocol struct {
	writeBufferSize uint16 // write buffer size that must be a divisor of 65536
	readBufferSize  uint16 // read buffer size that must be a divisor of 65536

	pool *Pool

	ph ProtocolPacketHandler
	eh ProtocolErrorHandler

	mu   sync.Mutex    // mutex over everything
	die  bool          // is this conn closed?
	exit chan struct{} // signal channel to close the conn

	lui uint16    // last sent packet index that hasn't been sent via an ack yet
	oui uint16    // oldest sent packet index that hasn't been acked yet
	ouc sync.Cond // stop writes if the next write given oui may flood our peers read buffer
	ls  time.Time // last time data was sent to our peer

	wi uint16 // write index
	ri uint16 // read index

	wq []uint32 // write queue
	rq []uint32 // read queue

	wqe []writtenPacket // write queue entries
}

func NewProtocol(opts ...ProtocolOption) *Protocol {
	p := &Protocol{exit: make(chan struct{})}

	for _, opt := range opts {
		opt.applyProtocol(p)
	}

	if p.writeBufferSize == 0 {
		p.writeBufferSize = DefaultWriteBufferSize
	}

	if p.readBufferSize == 0 {
		p.readBufferSize = DefaultReadBufferSize
	}

	if p.pool == nil {
		p.pool = new(Pool)
	}

	p.wq = make([]uint32, p.writeBufferSize)
	p.rq = make([]uint32, p.readBufferSize)

	emptyBufferIndices(p.wq)
	emptyBufferIndices(p.rq)

	p.wqe = make([]writtenPacket, p.writeBufferSize)

	p.ouc.L = &p.mu

	return p
}

func (p *Protocol) WritePacket(reliable bool, buf []byte) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var (
		idx     uint16
		ack     uint16
		ackBits uint32
		ok      = true
	)

	if reliable {
		idx, ack, ackBits, ok = p.waitForNextWriteDetails()
	} else {
		ack, ackBits = p.nextAckDetails()
	}

	if !ok {
		return nil, io.EOF
	}

	p.trackAcked(ack)

	// log.Printf("%p: send    (seq=%05d) (ack=%05d) (ack_bits=%032b) (size=%d) (reliable=%t)", p, idx, ack, ackBits, len(buf), reliable)

	return p.write(PacketHeader{Sequence: idx, ACK: ack, ACKBits: ackBits, Unordered: !reliable}, buf), nil
}

func (p *Protocol) waitUntilReaderAvailable() {
	for !p.die && seq.GT(p.wi+1, p.oui+uint16(len(p.rq))) {
		p.ouc.Wait()
	}
}

func (p *Protocol) waitForNextWriteDetails() (idx uint16, ack uint16, ackBits uint32, ok bool) {
	p.waitUntilReaderAvailable()

	idx, ok = p.nextWriteIndex(), !p.die
	ack, ackBits = p.nextAckDetails()
	return idx, ack, ackBits, ok
}

func (p *Protocol) nextWriteIndex() (idx uint16) {
	idx, p.wi = p.wi, p.wi+1
	return idx
}

func (p *Protocol) nextAckDetails() (ack uint16, ackBits uint32) {
	ack = p.ri - 1
	ackBits = p.prepareAckBits(ack)
	return ack, ackBits
}

func (p *Protocol) prepareAckBits(ack uint16) (ackBits uint32) {
	for i, m := uint16(0), uint32(1); i < ACKBitsetSize; i, m = i+1, m<<1 {
		if p.rq[(ack-i)%uint16(len(p.rq))] != uint32(ack-i) {
			continue
		}

		ackBits |= m
	}
	return ackBits
}

func (p *Protocol) write(header PacketHeader, buf []byte) []byte {
	b := p.pool.Get()

	b.B = header.AppendTo(b.B)
	b.B = append(b.B, buf...)

	if header.Unordered {
		defer p.pool.Put(b)
	}

	if !header.Unordered {
		p.trackWrite(header.Sequence, b)
	}

	return b.B
}

func (p *Protocol) trackWrite(idx uint16, buf *Buffer) {
	if seq.GT(idx+1, p.wi) {
		p.clearWrites(p.wi, idx)
		p.wi = idx + 1
	}

	i := idx % uint16(len(p.wq))
	p.wq[i] = uint32(idx)
	if p.wqe[i].buf != nil {
		p.pool.Put(p.wqe[i].buf)
	}
	p.wqe[i].buf = buf
	p.wqe[i].acked = false
	p.wqe[i].written = time.Now()
	p.wqe[i].resent = 0
}

func (p *Protocol) clearWrites(start, end uint16) {
	count, size := end-start+1, uint16(len(p.wq))

	if count >= size {
		emptyBufferIndices(p.wq)
		return
	}

	first := p.wq[start%size:]
	length := uint16(len(first))

	if count <= length {
		emptyBufferIndices(first[:count])
		return
	}

	second := p.wq[:count-length]

	emptyBufferIndices(first)
	emptyBufferIndices(second)
}

func (p *Protocol) ReadPacket(header PacketHeader, buf []byte) (needed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.readAckBits(header.ACK, header.ACKBits)

	if !header.Unordered && !p.trackRead(header.Sequence) {
		return
	}

	p.trackUnacked()

	if header.Empty {
		return
	}

	if p.ph != nil {
		p.ph(buf, header.Sequence)
	}

	// log.Printf("%p: recv    (seq=%05d) (ack=%05d) (ack_bits=%032b) (size=%d) (reliable=%t)", p, header.Sequence, header.ACK, header.ACKBits, len(buf), !header.Unordered)

	needed = p.checkIfAck()
	return
}

func (p *Protocol) checkIfAck() bool {
	lui := p.lui

	for i := uint16(0); i < ACKBitsetSize; i++ {
		if p.rq[(lui+i)%uint16(len(p.rq))] != uint32(lui+i) {
			return false
		}
	}

	return !p.die
}

func (p *Protocol) createAck() (header PacketHeader) {
	lui := p.lui + ACKBitsetSize
	p.lui = lui
	p.ls = time.Now()

	p.waitUntilReaderAvailable()

	header.Sequence, header.ACK = p.nextWriteIndex(), lui-1
	header.ACKBits = p.prepareAckBits(header.ACK)
	header.Empty = true

	return header
}

func (p *Protocol) readAckBits(ack uint16, ackBits uint32) {
	for idx := uint16(0); idx < ACKBitsetSize; idx, ackBits = idx+1, ackBits>>1 {
		if ackBits&1 == 0 {
			continue
		}

		i := (ack - idx) % uint16(len(p.wq))
		if p.wq[i] != uint32(ack-idx) || p.wqe[i].acked {
			continue
		}

		if p.wqe[i].buf != nil {
			p.pool.Put(p.wqe[i].buf)
		}

		p.wqe[i].buf = nil
		p.wqe[i].acked = true
	}
}

func (p *Protocol) trackRead(idx uint16) bool {
	i := idx % uint16(len(p.rq))

	if p.rq[i] == uint32(idx) { // duplicate packet
		return false
	}

	if seq.GT(idx+1, p.ri) {
		p.clearReads(p.ri, idx)
		p.ri = idx + 1
	}

	p.rq[i] = uint32(idx)

	return true
}

func (p *Protocol) clearReads(start, end uint16) {
	count, size := end-start+1, uint16(len(p.rq))

	if count >= size {
		emptyBufferIndices(p.rq)
		return
	}

	first := p.rq[start%size:]
	length := uint16(len(first))

	if count <= length {
		emptyBufferIndices(first[:count])
		return
	}

	second := p.rq[:count-length]

	emptyBufferIndices(first)
	emptyBufferIndices(second)
}

func (p *Protocol) trackAcked(ack uint16) {
	lui := p.lui

	for lui <= ack {
		if p.rq[lui%uint16(len(p.rq))] != uint32(lui) {
			break
		}
		lui++
	}

	p.lui = lui
	p.ls = time.Now()
}

func (p *Protocol) trackUnacked() {
	oui := p.oui

	for {
		i := oui % uint16(len(p.wq))
		if p.wq[i] != uint32(oui) || !p.wqe[i].acked {
			break
		}
		oui++
	}
	p.oui = oui

	p.ouc.Broadcast()
}

func (p *Protocol) close() bool {
	if p.die {
		return false
	}
	p.die = true
	p.ouc.Broadcast()

	return true
}

func (p *Protocol) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.close() {
		return
	}
}

func (p *Protocol) checkIfRetransmit(idx uint16, resendTimeout time.Duration) ([]byte, bool) {
	i := (p.oui + idx) % uint16(len(p.wq))
	if p.wq[i] != uint32(p.oui+idx) || !p.wqe[i].shouldResend(time.Now(), resendTimeout) {
		return nil, false
	}
	return p.wqe[i].buf.B, true
}

func (p *Protocol) incrementWqe(idx uint16) {
	i := (p.oui + idx) % uint16(len(p.wq))
	p.wqe[i].written = time.Now()
	p.wqe[i].resent++
}

func (p *Protocol) callErrorHandler(err error) {
	if p.eh != nil {
		p.eh(err)
	}
}

func (p *Protocol) writeQueueLen() uint16 {
	return uint16(len(p.wq))
}
