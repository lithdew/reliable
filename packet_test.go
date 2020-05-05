package reliable

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/bytebufferpool"
	"math"
	"testing"
	"testing/quick"
)

func TestEncodeDecodePacketHeader(t *testing.T) {
	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	f := func(seq, ack uint16, ackBits uint32) bool {
		header := PacketHeader{seq: seq, ack: ack, ackBits: ackBits}
		recovered, leftover, err := UnmarshalPacketHeader(header.AppendTo(buf.B[:0]))
		return assert.NoError(t, err) && assert.Len(t, leftover, 0) && assert.EqualValues(t, header, recovered)
	}

	require.NoError(t, quick.Check(f, &quick.Config{MaxCount: 1000}))
}

func TestEncodeDecodeFragmentHeader(t *testing.T) {
	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	f := func(seq uint16, id, total uint8) bool {
		header := FragmentHeader{seq: seq, id: id, total: total}
		recovered, leftover, err := UnmarshalFragmentHeader(header.AppendTo(buf.B[:0]))
		return assert.NoError(t, err) && assert.Len(t, leftover, 0) && assert.EqualValues(t, header, recovered)
	}

	require.NoError(t, quick.Check(f, &quick.Config{MaxCount: 1000}))
}

func BenchmarkMarshalPacketHeader(b *testing.B) {
	header := PacketHeader{seq: math.MaxUint16, ack: math.MaxUint16, ackBits: math.MaxUint32}

	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf.B = header.AppendTo(buf.B[:0])
	}
}

func BenchmarkUnmarshalPacketHeader(b *testing.B) {
	header := PacketHeader{seq: math.MaxUint16, ack: math.MaxUint16, ackBits: math.MaxUint32}

	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	buf.B = header.AppendTo(buf.B)

	var (
		recovered PacketHeader
		leftover  []byte
		err       error
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		recovered, leftover, err = UnmarshalPacketHeader(buf.B)
		if err != nil {
			b.Fatalf("failed to unmarshal packet header: %s", err)
		}
		if leftover := len(leftover); leftover != 0 {
			b.Fatalf("got %d byte(s) leftover", leftover)
		}
		if recovered.seq != header.seq || recovered.ack != header.ack || recovered.ackBits != header.ackBits {
			b.Fatalf("got %#v, expected %#v", recovered, header)
		}
	}

	_ = recovered
}
