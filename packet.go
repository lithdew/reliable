package reliable

import (
	"github.com/lithdew/bytesutil"
	"github.com/valyala/bytebufferpool"
	"io"
	"math/bits"
	"time"
)

const ACKBitsetSize = 32

type (
	Buffer = bytebufferpool.ByteBuffer
	Pool   = bytebufferpool.Pool
)

type writtenPacket struct {
	buf     *Buffer   // pooled contents of this packet
	acked   bool      // whether or not this packet was acked
	written time.Time // last time the packet was written
	resent  byte      // total number of times this packet was resent
}

func (p writtenPacket) shouldResend(now time.Time, ackTimeout time.Duration) bool {
	return !p.acked && p.resent < 10 && now.Sub(p.written) >= ackTimeout
}

type PacketHeaderFlag uint8

const (
	FlagFragment PacketHeaderFlag = 1 << iota
	FlagA
	FlagB
	FlagC
	FlagD
	FlagACKEncoded
	FlagEmpty
	FlagUnordered
)

func (p PacketHeaderFlag) Toggle(flag PacketHeaderFlag) PacketHeaderFlag {
	return p | flag
}

func (p PacketHeaderFlag) Toggled(flag PacketHeaderFlag) bool {
	return p&flag != 0
}

func (p PacketHeaderFlag) AppendTo(dst []byte) []byte {
	return append(dst, byte(p))
}

type PacketHeader struct {
	Sequence  uint16
	ACK       uint16
	ACKBits   uint32
	Unordered bool
	Empty     bool
}

func (p PacketHeader) AppendTo(dst []byte) []byte {
	// Mark a flag byte to RLE-encode the ACK bitset.

	flag := PacketHeaderFlag(0)
	if p.ACKBits&0x000000FF != 0x000000FF {
		flag = flag.Toggle(FlagA)
	}
	if p.ACKBits&0x0000FF00 != 0x0000FF00 {
		flag = flag.Toggle(FlagB)
	}
	if p.ACKBits&0x00FF0000 != 0x00FF0000 {
		flag = flag.Toggle(FlagC)
	}
	if p.ACKBits&0xFF000000 != 0xFF000000 {
		flag = flag.Toggle(FlagD)
	}
	if p.Empty {
		flag = flag.Toggle(FlagEmpty)
	}
	if p.Unordered {
		flag = flag.Toggle(FlagUnordered)
	}

	diff := int(p.Sequence) - int(p.ACK)
	if diff < 0 {
		diff += 65536
	}
	if diff <= 255 {
		flag = flag.Toggle(FlagACKEncoded)
	}

	// If the difference between the sequence number and the latest ACK'd sequence number can be represented by a
	// single byte, then represent it as a single byte and set the 5th bit of flag.

	// Marshal the flag and sequence number and latest ACK'd sequence number.

	dst = flag.AppendTo(dst)

	if p.Unordered {
		dst = bytesutil.AppendUint16BE(dst, p.ACK)
	} else {
		dst = bytesutil.AppendUint16BE(dst, p.Sequence)

		if diff <= 255 {
			dst = append(dst, uint8(diff))
		} else {
			dst = bytesutil.AppendUint16BE(dst, p.ACK)
		}
	}

	// Marshal ACK bitset.

	if p.ACKBits&0x000000FF != 0x000000FF {
		dst = append(dst, uint8(p.ACKBits&0x000000FF))
	}
	if p.ACKBits&0x0000FF00 != 0x0000FF00 {
		dst = append(dst, uint8((p.ACKBits&0x0000FF00)>>8))
	}
	if p.ACKBits&0x00FF0000 != 0x00FF0000 {
		dst = append(dst, uint8((p.ACKBits&0x00FF0000)>>16))
	}
	if p.ACKBits&0xFF000000 != 0xFF000000 {
		dst = append(dst, uint8((p.ACKBits&0xFF000000)>>24))
	}

	return dst
}

func UnmarshalPacketHeader(buf []byte) (header PacketHeader, leftover []byte, err error) {
	flag := PacketHeaderFlag(0)

	// Read first 3 bytes (header, flag).

	if len(buf) < 3 {
		return header, buf, io.ErrUnexpectedEOF
	}

	flag, buf = PacketHeaderFlag(buf[0]), buf[1:]

	if flag.Toggled(FlagFragment) {
		return header, buf, io.ErrUnexpectedEOF
	}

	header.Empty = flag.Toggled(FlagEmpty)
	header.Unordered = flag.Toggled(FlagUnordered)

	if header.Unordered {
		if len(buf) < 2 {
			return header, buf, io.ErrUnexpectedEOF
		}
		header.ACK, buf = bytesutil.Uint16BE(buf[:2]), buf[2:]
	} else {
		header.Sequence, buf = bytesutil.Uint16BE(buf[:2]), buf[2:]

		// Read and decode the latest ACK'ed sequence number (either 1 or 2 bytes) using the RLE flag marker.

		if flag.Toggled(FlagACKEncoded) {
			if len(buf) < 1 {
				return header, buf, io.ErrUnexpectedEOF
			}
			header.ACK, buf = header.Sequence-uint16(buf[0]), buf[1:]
		} else {
			if len(buf) < 2 {
				return header, buf, io.ErrUnexpectedEOF
			}
			header.ACK, buf = bytesutil.Uint16BE(buf[:2]), buf[2:]
		}
	}

	if len(buf) < bits.OnesCount8(uint8(flag&(FlagA|FlagB|FlagC|FlagD))) {
		return header, buf, io.ErrUnexpectedEOF
	}

	// Read and decode ACK bitset using the RLE flag marker.

	header.ACKBits = 0xFFFFFFFF

	if flag.Toggled(FlagA) {
		header.ACKBits &= 0xFFFFFF00
		header.ACKBits |= uint32(buf[0])
		buf = buf[1:]
	}

	if flag.Toggled(FlagB) {
		header.ACKBits &= 0xFFFF00FF
		header.ACKBits |= uint32(buf[0]) << 8
		buf = buf[1:]
	}

	if flag.Toggled(FlagC) {
		header.ACKBits &= 0xFF00FFFF
		header.ACKBits |= uint32(buf[0]) << 16
		buf = buf[1:]
	}

	if flag.Toggled(FlagD) {
		header.ACKBits &= 0x00FFFFFF
		header.ACKBits |= uint32(buf[0]) << 24
		buf = buf[1:]
	}

	return header, buf, nil
}
