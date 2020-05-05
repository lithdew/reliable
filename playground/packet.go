package main

import (
	"github.com/lithdew/bytesutil"
	"io"
	"math/bits"
)

type PacketHeaderFlag uint8

const (
	FlagFragment PacketHeaderFlag = 1 << iota
	FlagA
	FlagB
	FlagC
	FlagD
	FlagACKEncoded
	FlagACK
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
	seq       uint16
	ack       uint16
	ackBits   uint32
	unordered bool
	acked     bool
}

func (p PacketHeader) AppendTo(dst []byte) []byte {
	// Mark a flag byte to RLE-encode the ACK bitset.

	flag := PacketHeaderFlag(0)
	if p.ackBits&0x000000FF != 0x000000FF {
		flag = flag.Toggle(FlagA)
	}
	if p.ackBits&0x0000FF00 != 0x0000FF00 {
		flag = flag.Toggle(FlagB)
	}
	if p.ackBits&0x00FF0000 != 0x00FF0000 {
		flag = flag.Toggle(FlagC)
	}
	if p.ackBits&0xFF000000 != 0xFF000000 {
		flag = flag.Toggle(FlagD)
	}
	if p.acked {
		flag = flag.Toggle(FlagACK)
	}
	if p.unordered {
		flag = flag.Toggle(FlagUnordered)
	}

	diff := int(p.seq) - int(p.ack)
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

	if p.unordered {
		dst = bytesutil.AppendUint16BE(dst, p.ack)
	} else {
		dst = bytesutil.AppendUint16BE(dst, p.seq)

		if diff <= 255 {
			dst = append(dst, uint8(diff))
		} else {
			dst = bytesutil.AppendUint16BE(dst, p.ack)
		}
	}

	// Marshal ACK bitset.

	if p.ackBits&0x000000FF != 0x000000FF {
		dst = append(dst, uint8(p.ackBits&0x000000FF))
	}
	if p.ackBits&0x0000FF00 != 0x0000FF00 {
		dst = append(dst, uint8((p.ackBits&0x0000FF00)>>8))
	}
	if p.ackBits&0x00FF0000 != 0x00FF0000 {
		dst = append(dst, uint8((p.ackBits&0x00FF0000)>>16))
	}
	if p.ackBits&0xFF000000 != 0xFF000000 {
		dst = append(dst, uint8((p.ackBits&0xFF000000)>>24))
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

	header.acked = flag.Toggled(FlagACK)
	header.unordered = flag.Toggled(FlagUnordered)

	if header.unordered {
		if len(buf) < 2 {
			return header, buf, io.ErrUnexpectedEOF
		}
		header.ack, buf = bytesutil.Uint16BE(buf[:2]), buf[2:]
	} else {
		header.seq, buf = bytesutil.Uint16BE(buf[:2]), buf[2:]

		// Read and decode the latest ACK'ed sequence number (either 1 or 2 bytes) using the RLE flag marker.

		if flag.Toggled(FlagACKEncoded) {
			if len(buf) < 1 {
				return header, buf, io.ErrUnexpectedEOF
			}
			header.ack, buf = header.seq-uint16(buf[0]), buf[1:]
		} else {
			if len(buf) < 2 {
				return header, buf, io.ErrUnexpectedEOF
			}
			header.ack, buf = bytesutil.Uint16BE(buf[:2]), buf[2:]
		}
	}

	if len(buf) < bits.OnesCount8(uint8(flag&(FlagA|FlagB|FlagC|FlagD))) {
		return header, buf, io.ErrUnexpectedEOF
	}

	// Read and decode ACK bitset using the RLE flag marker.

	header.ackBits = 0xFFFFFFFF

	if flag.Toggled(FlagA) {
		header.ackBits &= 0xFFFFFF00
		header.ackBits |= uint32(buf[0])
		buf = buf[1:]
	}

	if flag.Toggled(FlagB) {
		header.ackBits &= 0xFFFF00FF
		header.ackBits |= uint32(buf[0]) << 8
		buf = buf[1:]
	}

	if flag.Toggled(FlagC) {
		header.ackBits &= 0xFF00FFFF
		header.ackBits |= uint32(buf[0]) << 16
		buf = buf[1:]
	}

	if flag.Toggled(FlagD) {
		header.ackBits &= 0x00FFFFFF
		header.ackBits |= uint32(buf[0]) << 24
		buf = buf[1:]
	}

	return header, buf, nil
}
