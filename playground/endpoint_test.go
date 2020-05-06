package main

import (
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

func newPacketConn(t testing.TB, addr string) net.PacketConn {
	t.Helper()
	conn, err := net.ListenPacket("udp", addr)
	require.NoError(t, err)
	return conn
}

func TestEndpoint(t *testing.T) {
	defer goleak.VerifyNone(t)

	var mu sync.Mutex

	values := make(map[string]struct{})

	actual := uint64(0)
	expected := uint64(65536)

	handler := func(seq uint16, buf []byte) {
		atomic.AddUint64(&actual, 1)

		mu.Lock()
		_, exists := values[string(buf)]
		delete(values, string(buf))
		mu.Unlock()

		require.True(t, exists)
	}

	ca := newPacketConn(t, "127.0.0.1:0")
	cb := newPacketConn(t, "127.0.0.1:0")

	a := NewEndpoint(ca, handler)
	b := NewEndpoint(cb, handler)

	go a.Listen()
	go b.Listen()

	defer func() {
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

		require.NoError(t, a.WriteTo(data, b.Addr()))
	}
}

func TestEndpointEndToEnd(t *testing.T) {
	defer goleak.VerifyNone(t)

	actual := uint64(0)
	expected := uint64(512)

	handler := func(seq uint16, buf []byte) {
		atomic.AddUint64(&actual, 1)
	}

	ca := newPacketConn(t, "127.0.0.1:0")
	cb := newPacketConn(t, "127.0.0.1:0")

	a := NewEndpoint(ca, handler)
	b := NewEndpoint(cb, handler)

	go a.Listen()
	go b.Listen()

	defer func() {
		require.NoError(t, a.Close())
		require.NoError(t, b.Close())

		require.NoError(t, ca.Close())
		require.NoError(t, cb.Close())

		require.EqualValues(t, expected*2, atomic.LoadUint64(&actual))
	}()

	for i := uint64(0); i < expected; i++ {
		data := strconv.AppendUint(nil, i, 10)

		require.NoError(t, a.WriteTo(data, b.Addr()))
		require.NoError(t, b.WriteTo(data, a.Addr()))
	}
}
