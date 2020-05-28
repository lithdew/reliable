package reliable

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"math"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnWriteReliablePacket(t *testing.T) {
	defer goleak.VerifyNone(t)

	data := bytes.Repeat([]byte("x"), 1400)

	actual := uint64(0)
	expected := uint64(65536)

	a, _ := net.ListenPacket("udp", "127.0.0.1:0")
	b, _ := net.ListenPacket("udp", "127.0.0.1:0")

	handler := func(buf []byte, _ uint16) {
		atomic.AddUint64(&actual, 1)
		require.EqualValues(t, data, buf)
	}

	ca := NewConn(b.LocalAddr(), a, WithProtocolPacketHandler(handler))
	cb := NewConn(a.LocalAddr(), b, WithProtocolPacketHandler(handler))

	go readLoop(t, a, ca)
	go readLoop(t, b, cb)

	go ca.Run()
	go cb.Run()

	defer func() {
		// Note: Guarantee that all messages are deliverd
		time.Sleep(1 * time.Second)

		require.NoError(t, a.SetDeadline(time.Now().Add(1*time.Millisecond)))
		require.NoError(t, b.SetDeadline(time.Now().Add(1*time.Millisecond)))

		ca.Close()
		cb.Close()

		require.NoError(t, a.Close())
		require.NoError(t, b.Close())

		require.EqualValues(t, expected, atomic.LoadUint64(&actual))
	}()

	for i := uint64(0); i < expected; i++ {
		require.NoError(t, ca.WriteReliablePacket(data))
	}
}

func readLoop(t *testing.T, pc net.PacketConn, c *Conn) {
	var (
		n   int
		err error
	)

	buf := make([]byte, math.MaxUint16+1)
	for {
		n, _, err = pc.ReadFrom(buf)
		if err != nil {
			break
		}

		header, buf, err := UnmarshalPacketHeader(buf[:n])
		require.NoError(t, err)

		if err == nil {
			err = c.Read(header, buf)
			require.NoError(t, err)
		}
	}
}
