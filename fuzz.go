package reliable

import (
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

	ea := NewEndpoint(ca)
	eb := NewEndpoint(cb)

	go ea.Listen()
	go eb.Listen()

	for i := 0; i < 32; i++ {
		if err := ea.WriteReliablePacket(data, eb.Addr()); err != nil && !isEOF(err) {
			return 0
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
