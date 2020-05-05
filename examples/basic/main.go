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

func handler(_ net.Addr, _ uint16, buf []byte) {
	if len(buf) == 0 {
		return
	}

	if !bytes.Equal(buf, PacketData) {
		spew.Dump(buf)
		os.Exit(1)
	}
}

func newPacketConn() net.PacketConn {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	check(err)
	return conn
}

func main() {
	exit := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(4)

	a := reliable.NewEndpoint(newPacketConn(), reliable.WithHandler(handler))
	b := reliable.NewEndpoint(newPacketConn(), reliable.WithHandler(handler))

	defer func() {
		check(a.Close())
		check(b.Close())

		close(exit)

		wg.Wait()
	}()

	// The two goroutines below are to have endpoints A and B listen for new peers.

	go func() {
		defer wg.Done()
		a.Listen()
	}()

	go func() {
		defer wg.Done()
		b.Listen()
	}()

	// The two goroutines below have endpoint A spam endpoint B, and print out how
	// many packets of data are being sent per second.

	go func() {
		defer wg.Done()

		for {
			if err := a.WriteTo(PacketData, b.Addr()); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				check(err)
			}
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

				log.Printf("Sent %d packet(s) comprised of %.2f MiB worth of data.", numPackets, numBytes)
			}
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
}
