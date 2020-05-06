package reliable

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newPacketConn(t testing.TB, addr string) net.PacketConn {
	t.Helper()
	conn, err := net.ListenPacket("udp", addr)
	require.NoError(t, err)
	return conn
}

func BenchmarkEndpointWriteReliablePacket(b *testing.B) {
	ca := newPacketConn(b, "127.0.0.1:0")
	cb := newPacketConn(b, "127.0.0.1:0")

	ea := NewEndpoint(ca)
	eb := NewEndpoint(cb)

	go ea.Listen()
	go eb.Listen()

	defer func() {
		require.NoError(b, ca.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(b, cb.SetDeadline(time.Now().Add(1*time.Millisecond)))

		require.NoError(b, ea.Close())
		require.NoError(b, eb.Close())

		require.NoError(b, ca.Close())
		require.NoError(b, cb.Close())
	}()

	data := bytes.Repeat([]byte("x"), 1400)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := ea.WriteReliablePacket(data, eb.Addr()); err != nil && !isEOF(err) {
			b.Fatal(err)
		}
	}
}

func BenchmarkEndpointWriteUnreliablePacket(b *testing.B) {
	ca := newPacketConn(b, "127.0.0.1:0")
	cb := newPacketConn(b, "127.0.0.1:0")

	ea := NewEndpoint(ca)
	eb := NewEndpoint(cb)

	go ea.Listen()
	go eb.Listen()

	defer func() {
		require.NoError(b, ca.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(b, cb.SetDeadline(time.Now().Add(1*time.Millisecond)))

		require.NoError(b, ea.Close())
		require.NoError(b, eb.Close())

		require.NoError(b, ca.Close())
		require.NoError(b, cb.Close())
	}()

	data := bytes.Repeat([]byte("x"), 1400)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := ea.WriteUnreliablePacket(data, eb.Addr()); err != nil && !isEOF(err) {
			b.Fatal(err)
		}
	}
}

func TestEndpointWriteReliablePacket(t *testing.T) {
	defer goleak.VerifyNone(t)

	var mu sync.Mutex

	values := make(map[string]struct{})

	actual := uint64(0)
	expected := uint64(65536)

	handler := func(_ net.Addr, seq uint16, buf []byte) {
		atomic.AddUint64(&actual, 1)

		mu.Lock()
		_, exists := values[string(buf)]
		delete(values, string(buf))
		mu.Unlock()

		require.True(t, exists)
	}

	ca := newPacketConn(t, "127.0.0.1:0")
	cb := newPacketConn(t, "127.0.0.1:0")

	a := NewEndpoint(ca, WithPacketHandler(handler))
	b := NewEndpoint(cb, WithPacketHandler(handler))

	go a.Listen()
	go b.Listen()

	defer func() {
		require.NoError(t, ca.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(t, cb.SetDeadline(time.Now().Add(1*time.Millisecond)))

		require.NoError(t, a.Close())
		require.NoError(t, b.Close())

		require.NoError(t, ca.Close())
		require.NoError(t, cb.Close())

		require.EqualValues(t, expected, atomic.LoadUint64(&actual))
	}()

	for i := uint64(0); i < expected; i++ {
		data := strconv.AppendUint(nil, i, 10)

		mu.Lock()
		values[string(data)] = struct{}{}
		mu.Unlock()

		require.NoError(t, a.WriteReliablePacket(data, b.Addr()))
	}
}

func TestEndpointWriteReliablePacketEndToEnd(t *testing.T) {
	defer goleak.VerifyNone(t)

	actual := uint64(0)
	expected := uint64(512)

	handler := func(_ net.Addr, seq uint16, buf []byte) {
		atomic.AddUint64(&actual, 1)
	}

	ca := newPacketConn(t, "127.0.0.1:0")
	cb := newPacketConn(t, "127.0.0.1:0")

	a := NewEndpoint(ca, WithPacketHandler(handler))
	b := NewEndpoint(cb, WithPacketHandler(handler))

	go a.Listen()
	go b.Listen()

	defer func() {
		require.NoError(t, ca.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(t, cb.SetDeadline(time.Now().Add(1*time.Millisecond)))

		require.NoError(t, a.Close())
		require.NoError(t, b.Close())

		require.NoError(t, ca.Close())
		require.NoError(t, cb.Close())

		require.EqualValues(t, expected*2, atomic.LoadUint64(&actual))
	}()

	for i := uint64(0); i < expected; i++ {
		data := strconv.AppendUint(nil, i, 10)

		require.NoError(t, a.WriteReliablePacket(data, b.Addr()))
		require.NoError(t, b.WriteReliablePacket(data, a.Addr()))
	}
}
