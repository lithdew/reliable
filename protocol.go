package reliable

import (
  "fmt"
  "github.com/lithdew/seq"
  "io"
  "net"
  "sync"
  "time"
)

type Protocol struct {
  writeBufferSize uint16 // write buffer size that must be a divisor of 65536
  readBufferSize  uint16 // read buffer size that must be a divisor of 65536

  updatePeriod  time.Duration // how often time-dependant parts of the protocol get checked
  resendTimeout time.Duration // how long we wait until unacked packets should be resent

  addr net.Addr
  pool *Pool

  ph PacketHandler
  eh ErrorHandler

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

func NewProtocol(addr net.Addr, opts ...ProtocolOption) *Conn {
  p := &Protocol{addr: addr, exit: make(chan struct{})}

  for _, opt := range opts {
    opt.applyConn(p)
  }

  if p.writeBufferSize == 0 {
    p.writeBufferSize = DefaultWriteBufferSize
  }

  if p.readBufferSize == 0 {
    p.readBufferSize = DefaultReadBufferSize
  }

  if p.resendTimeout == 0 {
    p.resendTimeout = DefaultResendTimeout
  }

  if p.updatePeriod == 0 {
    p.updatePeriod = DefaultUpdatePeriod
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

func (p *Protocol) writePacket(reliable bool, buf []byte, w writeFunc) error {
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
    return io.EOF
  }

  p.trackAcked(ack)

  if err := p.write(PacketHeader{Sequence: idx, ACK: ack, ACKBits: ackBits, Unordered: !reliable}, buf, w); err != nil {
    return err
  }

  //log.Printf("%s: send    (seq=%05d) (ack=%05d) (ack_bits=%032b) (size=%d) (reliable=%t)", p.conn.LocalAddr(), idx, ack, ackBits, len(buf), reliable)

  return nil
}

func (p *Protocol) waitUntilReaderAvailable() {
  for !p.die && seq.GT(p.wi+1, p.oui+uint16(len(p.rq))) {
    p.ouc.Wait()
  }
}

func (p *Protocol) waitForNextWriteDetails() (idx uint16, ack uint16, ackBits uint32, ok bool) {
  p.mu.Lock()
  defer p.mu.Unlock()

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

func (p *Protocol) write(header PacketHeader, buf []byte, w writeFunc) error {
  b := p.pool.Get()

  b.B = header.AppendTo(b.B)
  b.B = append(b.B, buf...)

  if header.Unordered {
    defer p.pool.Put(b)
  }

  if !header.Unordered {
    p.trackWrite(header.Sequence, b)
  }

  if err := w(p.addr, b.B); err != nil && !isEOF(err) {
    return fmt.Errorf("failed to transmit packet: %w", err)
  }

  return nil
}

func (p *Protocol) trackWrite(idx uint16, buf *Buffer) {
  p.mu.Lock()
  defer p.mu.Unlock()

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

func (p *Protocol) Read(header PacketHeader, buf []byte, w writeFunc) error {
  p.readAckBits(header.ACK, header.ACKBits)

  if !header.Unordered && !p.trackRead(header.Sequence) {
    return nil
  }

  p.trackUnacked()

  if err := p.writeAcksIfNecessary(w); err != nil {
    return fmt.Errorf("failed to write acks when necessary: %w", err)
  }

  if header.Empty {
    return nil
  }

  if p.ph != nil {
    p.ph(p.addr, header.Sequence, buf)
  }

  //log.Printf("%s: recv    (seq=%05d) (ack=%05d) (ack_bits=%032b) (size=%d) (reliable=%t)", p.conn.LocalAddr(), header.Sequence, header.ACK, header.ACKBits, len(buf), !header.Unordered)

  return nil
}

func (p *Protocol) createAckIfNecessary() (header PacketHeader, needed bool) {
  p.mu.Lock()
  defer p.mu.Unlock()

  lui := p.lui

  for i := uint16(0); i < ACKBitsetSize; i++ {
    if p.rq[(lui+i)%uint16(len(p.rq))] != uint32(lui+i) {
      return header, needed
    }
  }

  lui += ACKBitsetSize
  p.lui = lui
  p.ls = time.Now()

  p.waitUntilReaderAvailable()

  header.Sequence, header.ACK = p.nextWriteIndex(), lui-1
  header.ACKBits = p.prepareAckBits(header.ACK)
  header.Empty = true

  needed = !p.die

  return header, needed
}

func (p *Protocol) writeAcksIfNecessary(w writeFunc) error {
  for {
    header, needed := p.createAckIfNecessary()
    if !needed {
      return nil
    }

    //log.Printf("%s: ack     (seq=%05d) (ack=%05d) (ack_bits=%032b)", p.conn.LocalAddr(), header.Sequence, header.ACK, header.ACKBits)

    if err := p.write(header, nil, w); err != nil {
      return fmt.Errorf("failed to write ack packet: %w", err)
    }
  }
}

func (p *Protocol) readAckBits(ack uint16, ackBits uint32) {
  p.mu.Lock()
  defer p.mu.Unlock()

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
  p.mu.Lock()
  defer p.mu.Unlock()

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
  p.mu.Lock()
  defer p.mu.Unlock()

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
  p.mu.Lock()
  defer p.mu.Unlock()

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
  p.mu.Lock()
  defer p.mu.Unlock()

  if p.die {
    return false
  }
  close(p.exit)
  p.die = true
  p.ouc.Broadcast()

  return true
}

func (p *Protocol) Close() {
  if !p.close() {
    return
  }

  //p.mu.Lock()
  //defer p.mu.Unlock()

  //if strings.Contains(p.conn.LocalAddr().String(), "44444") { // sending
  //log.Printf("send closed (oldest_sent_ack_idx=%05d) (oldest_unacked_idx=%05d)", p.lui, p.oui)
  //} else if strings.Contains(p.conn.LocalAddr().String(), "55555") { // receiving
  //log.Printf("recv closed (oldest_sent_ack_idx=%05d) (oldest_unacked_idx=%05d)", p.lui, p.oui)
  //}
}

func (p *Protocol) Run(w writeFunc) {
  ticker := time.NewTicker(p.updatePeriod)
  defer ticker.Stop()

  for {
    select {
    case <-p.exit:
      return
    case <-ticker.C:
      if err := p.retransmitUnackedPackets(w); err != nil && p.eh != nil {
        p.eh(p.addr, err)
      }
    }
  }
}

func (p *Protocol) retransmitUnackedPackets(w writeFunc) error {
  p.mu.Lock()
  defer p.mu.Unlock()

  for idx := uint16(0); idx < uint16(len(p.wq)); idx++ {
    i := (p.oui + idx) % uint16(len(p.wq))
    if p.wq[i] != uint32(p.oui+idx) || !p.wqe[i].shouldResend(time.Now(), p.resendTimeout) {
      continue
    }

    //log.Printf("%s: resend  (seq=%d)", p.conn.LocalAddr(), p.oui+idx)

    if err := w(p.addr, p.wqe[i].buf.B); err != nil {
      if isEOF(err) {
        break
      }
      return fmt.Errorf("failed to retransmit unacked packet: %w", err)
    }

    p.wqe[i].written = time.Now()
    p.wqe[i].resent++
  }

  return nil
}
