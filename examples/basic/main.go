package main

import (
	"bytes"
	"errors"
	"github.com/davecgh/go-spew/spew"
	"github.com/lithdew/reliable"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"
)

var (
	PacketData = bytes.Repeat([]byte("a"), 1400)
	NumPackets = uint64(0)
)

func check(err error) {
	if err != nil && !errors.Is(err, io.EOF) {
		log.Panic(err)
	}
}

func listen(addr string) net.PacketConn {
	conn, err := net.ListenPacket("udp", addr)
	check(err)
	return conn
}

func handler(_ net.Addr, _ uint16, buf []byte) {
	if bytes.Equal(buf, PacketData) {
		return
	}
	spew.Dump(buf)
	os.Exit(1)
}

func main() {
	exit := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(2)

	ca := listen("127.0.0.1:44444")
	cb := listen("127.0.0.1:55555")

	a := reliable.NewEndpoint(ca, reliable.WithHandler(handler))
	b := reliable.NewEndpoint(cb, reliable.WithHandler(handler))

	defer func() {
		close(exit)

		check(a.Close())
		check(b.Close())

		check(ca.Close())
		check(cb.Close())

		wg.Wait()
	}()

	go a.Listen()
	go b.Listen()

	// The two goroutines below have endpoint A spam endpoint B, and print out how
	// many packets of data are being sent per second.

	go func() {
		defer wg.Done()

		for {
			select {
			case <-exit:
				return
			default:
			}

			check(a.WriteReliablePacket(PacketData, b.Addr()))
			atomic.AddUint64(&NumPackets, 1)
		}
	}()

	go func() {
		defer wg.Done()

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-exit:
				return
			case <-ticker.C:
				numPackets := atomic.SwapUint64(&NumPackets, 0)
				numBytes := float64(numPackets) * 1400.0 / 1024.0 / 1024.0

				log.Printf(
					"Sent %d packet(s) comprised of %.2f MiB worth of data.",
					numPackets,
					numBytes,
				)
			}
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
}
