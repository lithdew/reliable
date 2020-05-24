package reliable

import (
	"net"
	"time"
)

func closeConn(c net.PacketConn, e *Endpoint, ch chan error) {
	c.SetDeadline(time.Now().Add(1 * time.Millisecond))

	if err := e.Close(); err != nil {
		ch <- err
	}

	if err := c.Close(); err != nil {
		ch <- err
	}

	ch <- nil
}

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

	chErr := make(chan error, 2)

	go closeConn(ca, ea, chErr)
	go closeConn(cb, eb, chErr)

	for i := 0; i < cap(chErr); i++ {
		if err := <-chErr; err != nil {
			return 0
		}
	}

	return 1
}
