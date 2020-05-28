package main

import (
	"bytes"
	"errors"
	"net"
	"time"
	"io"
	"github.com/lithdew/reliable"
)

func main() {
	var data []byte = []byte{}

	ca, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic("hoge")
	}
	cb, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic("hoge")
	}

	chErr := make(chan error)

	handler := func(buf []byte, _ net.Addr) {
		if len(buf) == 0 || bytes.Equal(buf, data) {
			return
		}
		panic("hoge")
		chErr <- errors.New("data miss match")
	}

	ea := reliable.NewEndpoint(ca, reliable.WithEndpointPacketHandler(handler))
	eb := reliable.NewEndpoint(cb, reliable.WithEndpointPacketHandler(handler))

	go ea.Listen()
	go eb.Listen()

	for i := 0; i < 65536; i++ {
		select {
		case <-chErr:
			panic("hoge")
		default:
			if err := ea.WriteReliablePacket(data, eb.Addr()); err != nil && !isEOF(err) {
				panic("hoge")
			}
		}
	}

	if err := ca.SetDeadline(time.Now().Add(1 * time.Millisecond)); err != nil {
		panic("hoge")
	}

	if err := cb.SetDeadline(time.Now().Add(1 * time.Millisecond)); err != nil {
		panic("hoge")
	}

	if err := ea.Close(); err != nil {
		panic("hoge")
	}
	if err := eb.Close(); err != nil {
		panic("hoge")
	}

	if err := ca.Close(); err != nil {
		panic("hoge")
	}
	if err := cb.Close(); err != nil {
		panic("hoge")
	}
}

func isEOF(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) {
		if netErr.Err.Error() == "use of closed network connection" {
			return true
		}
		if netErr.Timeout() {
			return true
		}
	}

	return false
}
