package reliable

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"net"
	"sync/atomic"
	"testing"
)

func newPacketConn(t testing.TB) net.PacketConn {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)

	return conn
}

func TestEndpointListenClose(t *testing.T) {
	defer goleak.VerifyNone(t)

	x := NewEndpoint(newPacketConn(t))
	y := NewEndpoint(newPacketConn(t))

	go x.Listen()
	go y.Listen()

	defer func() {
		require.NoError(t, x.Close())
		require.NoError(t, y.Close())
	}()
}

func TestEndpoint(t *testing.T) {
	defer goleak.VerifyNone(t)

	count := uint32(0)

	handler := func(_ net.Addr, seq uint16, buf []byte) {
		if len(buf) == 0 {
			return
		}
		atomic.AddUint32(&count, 1)
	}

	x := NewEndpoint(newPacketConn(t), WithHandler(handler))
	y := NewEndpoint(newPacketConn(t), WithHandler(handler))

	go x.Listen()
	go y.Listen()

	defer func() {
		require.NoError(t, x.Close())
		require.NoError(t, y.Close())

		//fmt.Println(atomic.LoadUint32(&count)) FIXME(kenta): the count must be 4096, but isn't
	}()

	data := bytes.Repeat([]byte("a"), 1400)

	for i := 0; i < 4096; i++ {
		require.NoError(t, x.WriteTo(data, y.Addr()))
	}
}

func TestEndpointEndToEnd(t *testing.T) {
	defer goleak.VerifyNone(t)

	x := NewEndpoint(newPacketConn(t))
	y := NewEndpoint(newPacketConn(t))

	go x.Listen()
	go y.Listen()

	defer func() {
		require.NoError(t, x.Close())
		require.NoError(t, y.Close())
	}()

	data := bytes.Repeat([]byte("a"), 1400)

	for i := 0; i < 4096; i++ {
		require.NoError(t, x.WriteTo(data, y.Addr()))
		require.NoError(t, y.WriteTo(data, x.Addr()))
	}
}

func BenchmarkEndpoint(b *testing.B) {
	x := NewEndpoint(newPacketConn(b))
	y := NewEndpoint(newPacketConn(b))

	go x.Listen()
	go y.Listen()

	defer func() {
		require.NoError(b, x.Close())
		require.NoError(b, y.Close())
	}()

	data := bytes.Repeat([]byte("a"), 1400)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := x.WriteTo(data, y.Addr()); err != nil {
			b.Fatal(err)
		}
	}
}
