package main

import (
	"bytes"
	"errors"
	"flag"
	"github.com/lithdew/reliable"
	"io"
	"log"
	"net"
	"sync/atomic"
	"time"
)

var (
	listener bool
)

func check(err error) {
	if err != nil && !errors.Is(err, io.EOF) {
		log.Panic(err)
	}
}

func listen(addr string) net.PacketConn {
	conn, err := net.ListenPacket("udp", addr)
	check(err)

	log.Printf("%s: Listening for peers.", conn.LocalAddr())

	return conn
}

func main() {
	flag.BoolVar(&listener, "l", false, "either listen or dial")
	flag.Parse()

	host := flag.Arg(0)
	if !listener || host == "" {
		host = ":0"
	}

	conn := listen(host)

	counter := uint64(0)

	handler := func(addr net.Addr, seq uint16, buf []byte) {
		//log.Printf("%s->%s: (seq=%d) (size=%d)", addr.String(), conn.LocalAddr().String(), seq, len(buf))
		atomic.AddUint64(&counter, 1)
	}

	endpoint := reliable.NewEndpoint(conn, reliable.WithPacketHandler(handler))
	go endpoint.Listen()

	defer func() {
		check(endpoint.Close())
		check(conn.Close())
	}()

	if listener {
		for range time.Tick(1 * time.Second) {
			numPackets := atomic.SwapUint64(&counter, 0)
			numBytes := float64(numPackets) * 1400.0 / 1024.0 / 1024.0

			log.Printf("%s: Received %d packets (%.2f MiB).", conn.LocalAddr(), numPackets, numBytes)
		}
	}

	addr, err := net.ResolveUDPAddr("udp", flag.Arg(0))
	check(err)

	data := bytes.Repeat([]byte("x"), 1400)

	go func() {
		for range time.Tick(1 * time.Second) {
			numPackets := atomic.SwapUint64(&counter, 0)
			numBytes := float64(numPackets) * 1400.0 / 1024.0 / 1024.0

			log.Printf("%s: Sent %d packets (%.2f MiB).", conn.LocalAddr(), numPackets, numBytes)
		}
	}()

	for {
		check(endpoint.WriteReliablePacket(data, addr))
		atomic.AddUint64(&counter, 1)
	}
}
