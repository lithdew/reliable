// +build gofuzz

package reliable

import (
	"bytes"
	"errors"
	"net"
	"time"
)

func Fuzz(data []byte) int {
	ca, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return -1
	}
	cb, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return -1
	}

	chErr := make(chan error)

	handler := func(_ net.Addr, _ uint16, buf []byte) {
		if bytes.Equal(buf, data) {
			return
		}
		chErr <- errors.New("data miss match")
	}

	ea := NewEndpoint(ca, reliable.WithPacketHandler(handler))
	eb := NewEndpoint(cb, reliable.WithPacketHandler(handler))

	go ea.Listen()
	go eb.Listen()

	for i := 0; i < 65536; i++ {
		select {
		case <-chErr:
			return 0
		default:
			if err := ea.WriteReliablePacket(data, eb.Addr()); err != nil && !isEOF(err) {
				return 0
			}
		}
	}

	if err := ca.SetDeadline(time.Now().Add(1 * time.Millisecond)); err != nil {
		return 0
	}

	if err := cb.SetDeadline(time.Now().Add(1 * time.Millisecond)); err != nil {
		return 0
	}

	if err := ea.Close(); err != nil {
		return 0
	}
	if err := eb.Close(); err != nil {
		return 0
	}

	if err := ca.Close(); err != nil {
		return 0
	}
	if err := cb.Close(); err != nil {
		return 0
	}

	return 1
}
