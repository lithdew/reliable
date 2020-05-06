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

func testConnWaitForWriteDetails(inc uint16) func(t testing.TB) {
	return func(t testing.TB) {
		defer goleak.VerifyNone(t)

		c := NewConn(nil, nil, nil, nil)
		c.wi = uint16(len(c.rq))

		var wg sync.WaitGroup
		wg.Add(8)

		ch := make(chan uint16, 8)

		for i := 0; i < 8; i++ {
			go func() {
				defer wg.Done()

				idx, _, _, _ := c.waitForNextWriteDetails()
				ch <- idx
			}()
		}

		for i := 0; i < 8; i++ {
			c.ouc.L.Lock()
			c.oui += inc
			c.ouc.Broadcast()
			c.ouc.L.Unlock()
		}

		wg.Wait()

		expected := make(map[uint16]struct{}, 8)

		close(ch)
		for idx := range ch {
			expected[idx] = struct{}{}
		}

		for i := 0; i < 8; i++ {
			actual := uint16(len(c.wq) + i)
			require.Contains(t, expected, actual)
			delete(expected, actual)
		}
	}
}

func TestConnWaitForWriteDetails(t *testing.T) {
	testConnWaitForWriteDetails(1)(t)
	testConnWaitForWriteDetails(2)(t)
	testConnWaitForWriteDetails(4)(t)
}

func newPacketConn(t testing.TB, addr string) net.PacketConn {
	t.Helper()
	conn, err := net.ListenPacket("udp", addr)
	require.NoError(t, err)
	return conn
}

func TestEndpointHandler(t *testing.T) {
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

func TestEndpointE2E(t *testing.T) {
	defer goleak.VerifyNone(t)

	var mu sync.Mutex

	values := make(map[string]struct{})

	actual := uint64(0)
	expected := uint64(512)

	handler := func(seq uint16, buf []byte) {
		atomic.AddUint64(&actual, 1)

		//mu.Lock()
		//_, exists := values[string(buf)]
		//delete(values, string(buf))
		//mu.Unlock()

		//require.True(t, exists)
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

		mu.Lock()
		values[string(data)] = struct{}{}
		mu.Unlock()

		require.NoError(t, a.WriteTo(data, b.Addr()))
		require.NoError(t, b.WriteTo(data, a.Addr()))
	}
}
