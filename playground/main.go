package main

import (
	"bytes"
	"log"
	"math"
	"net"
)

func listen(addr string) net.PacketConn {
	conn, err := net.ListenPacket("udp", addr)
	check(err)

	return conn
}

func main() {
	ca := listen("127.0.0.1:44444")
	cb := listen("127.0.0.1:55555")

	a := NewEndpoint(ca)
	b := NewEndpoint(cb)

	go a.Listen()
	go b.Listen()

	defer func() {
		check(a.Close())
		check(b.Close())

		check(ca.Close())
		check(cb.Close())

		log.Println("DONE")
	}()

	data := bytes.Repeat([]byte("a"), 1400)

	for i := uint64(0); i < math.MaxUint64; i++ {
		check(a.WriteTo(data, b.Addr()))
	}
}
