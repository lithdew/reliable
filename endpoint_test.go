package reliable

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"net"
	"sort"
	"strconv"
	"sync"
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

	var (
		expected []int
		actual   []int
		loop     uint64 = 65536
	)

	handler := func(buf []byte, _ net.Addr) {
		if len(buf) == 0 {
			return
		}
		num, _ := strconv.Atoi(string(buf))
		actual = append(actual, num)
	}

	ca := newPacketConn(t, "127.0.0.1:0")
	cb := newPacketConn(t, "127.0.0.1:0")

	a := NewEndpoint(ca, WithEndpointPacketHandler(handler))
	b := NewEndpoint(cb, WithEndpointPacketHandler(handler))

	go a.Listen()
	go b.Listen()

	defer func() {
		// Note: Guarantee that all messages are deliverd
		time.Sleep(1 * time.Second)

		require.NoError(t, ca.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(t, cb.SetDeadline(time.Now().Add(1*time.Millisecond)))

		require.NoError(t, a.Close())
		require.NoError(t, b.Close())

		require.NoError(t, ca.Close())
		require.NoError(t, cb.Close())

		require.EqualValues(t, expected, uniqSort(actual))
	}()

	for i := uint64(0); i < loop; i++ {
		data := strconv.AppendUint(nil, i, 10)
		expected = append(expected, int(i))

		require.NoError(t, a.WriteReliablePacket(data, b.Addr()))
	}
}

func TestEndpointWriteReliablePacketEndToEnd(t *testing.T) {
	defer goleak.VerifyNone(t)

	var (
		expected []int
		actual   []int
		loop     uint64 = 512
		mu       sync.Mutex
	)

	handler := func(buf []byte, _ net.Addr) {
		if len(buf) == 0 {
			return
		}
		mu.Lock()
		num, _ := strconv.Atoi(string(buf))
		actual = append(actual, num)
		mu.Unlock()
	}

	ca := newPacketConn(t, "127.0.0.1:0")
	cb := newPacketConn(t, "127.0.0.1:0")

	a := NewEndpoint(ca, WithEndpointPacketHandler(handler))
	b := NewEndpoint(cb, WithEndpointPacketHandler(handler))

	go a.Listen()
	go b.Listen()

	defer func() {
		require.NoError(t, ca.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(t, cb.SetDeadline(time.Now().Add(1*time.Millisecond)))

		require.NoError(t, a.Close())
		require.NoError(t, b.Close())

		require.NoError(t, ca.Close())
		require.NoError(t, cb.Close())

		sort.Ints(expected)
		require.EqualValues(t, expected, uniqSort(actual))
	}()

	for i := uint64(0); i < loop; i++ {
		dataA := strconv.AppendUint(nil, i, 10)
		dataB := strconv.AppendUint(nil, i+loop, 10)
		expected = append(expected, int(i), int(i+loop))

		require.NoError(t, a.WriteReliablePacket(dataA, b.Addr()))
		require.NoError(t, b.WriteReliablePacket(dataB, a.Addr()))
	}
}

// Check whether race condition happen
// Simulate write and read heavy condition by sending packet concurrently
func TestRaceConditions(t *testing.T) {
	defer goleak.VerifyNone(t)

	var expected int = 1000
	tr := newTestRaceConditions(expected)

	handler := func(buf []byte, _ net.Addr) {
		if len(buf) == 0 {
			return
		}
		tr.append(buf)
	}

	ca := newPacketConn(t, "127.0.0.1:0")
	cb := newPacketConn(t, "127.0.0.1:0")
	cc := newPacketConn(t, "127.0.0.1:0")
	cd := newPacketConn(t, "127.0.0.1:0")
	ce := newPacketConn(t, "127.0.0.1:0")

	a := NewEndpoint(ca, WithEndpointPacketHandler(handler))
	b := NewEndpoint(cb, WithEndpointPacketHandler(handler))
	c := NewEndpoint(cc, WithEndpointPacketHandler(handler))
	d := NewEndpoint(cd, WithEndpointPacketHandler(handler))
	e := NewEndpoint(ce, WithEndpointPacketHandler(handler))

	go a.Listen()
	go b.Listen()
	go c.Listen()
	go d.Listen()
	go e.Listen()

	defer func() {
		tr.wait()

		// Note: Guarantee that all messages are deliverd
		time.Sleep(100 * time.Millisecond)

		require.NoError(t, ca.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(t, cb.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(t, cc.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(t, cd.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(t, ce.SetDeadline(time.Now().Add(1*time.Millisecond)))

		require.NoError(t, a.Close())
		require.NoError(t, b.Close())
		require.NoError(t, c.Close())
		require.NoError(t, d.Close())
		require.NoError(t, e.Close())

		require.NoError(t, ca.Close())
		require.NoError(t, cb.Close())
		require.NoError(t, cc.Close())
		require.NoError(t, cd.Close())
		require.NoError(t, ce.Close())

		require.EqualValues(t, tr.expected, uniqSort(tr.actual))
	}()

	tr.wg.Add(1)
	sB := tr.expected[0 : len(tr.expected)/4]
	go func() {
		defer tr.done()
		for i := 0; i < len(sB); i++ {
			data := []byte(strconv.Itoa(sB[i]))

			err := a.WriteReliablePacket(data, b.Addr())
			if err != nil {
				require.True(t, isEOF(err))
			}
		}
	}()

	tr.wg.Add(1)
	sC := tr.expected[len(tr.expected)/4 : len(tr.expected)*2/4]
	go func() {
		defer tr.done()
		for i := 0; i < len(sC); i++ {
			data := []byte(strconv.Itoa(sC[i]))

			err := a.WriteReliablePacket(data, c.Addr())
			if err != nil {
				require.True(t, isEOF(err))
			}
		}
	}()

	tr.wg.Add(1)
	sD := tr.expected[len(tr.expected)*2/4 : len(tr.expected)*3/4]
	go func() {
		defer tr.done()
		for i := 0; i < len(sD); i++ {
			data := []byte(strconv.Itoa(sD[i]))

			err := a.WriteReliablePacket(data, d.Addr())
			if err != nil {
				require.True(t, isEOF(err))
			}
		}
	}()

	tr.wg.Add(1)
	sE := tr.expected[len(tr.expected)*3/4:]
	go func() {
		defer tr.done()
		for i := 0; i < len(sE); i++ {
			data := []byte(strconv.Itoa(sE[i]))

			err := a.WriteReliablePacket(data, e.Addr())
			if err != nil {
				require.True(t, isEOF(err))
			}
		}
	}()
}

// Note: This struct is test for TestRaceConditions
// The purpose for this struct is to prevent race condition of WaitGroup
type testRaceConditions struct {
	mu       sync.Mutex
	wg       sync.WaitGroup
	expected []int
	actual   []int
}

func newTestRaceConditions(cap int) *testRaceConditions {
	return &testRaceConditions{expected: genNumSlice(cap)}
}

func (t *testRaceConditions) done() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wg.Done()
}

func (t *testRaceConditions) wait() {
	t.wg.Wait()
}

func (t *testRaceConditions) append(buf []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	num, _ := strconv.Atoi(string(buf))
	t.actual = append(t.actual, num)
}

func genNumSlice(len int) (s []int) {
	for i := 0; i < len; i++ {
		s = append(s, i)
	}
	return
}

func uniqSort(s []int) (result []int) {
	sort.Ints(s)
	var pre int
	for i := 0; i < len(s); i++ {
		if i == 0 || s[i] != pre {
			result = append(result, s[i])
		}
		pre = s[i]
	}
	return
}
