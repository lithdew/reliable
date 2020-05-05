# reliable

[![MIT License](https://img.shields.io/apm/l/atomic-design-ui.svg?)](LICENSE)
[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lithdew/reliable)
[![Discord Chat](https://img.shields.io/discord/697002823123992617)](https://discord.gg/HZEbkeQ)

**reliable** is a reliability layer for UDP connections in Go.

It allows you to bootstrap over a UDP's susceptibility to packet loss, lack of ordering of packets, and granularly introduce packet ordering, peer acknowledgement over the recipient of packets, and more.

The reliability layer is based off of [reliable.io](https://github.com/networkprotocol/reliable.io), which is described further in detail by Glenn Fiedler's blog post [here](https://gafferongames.com/post/reliable_ordered_messages/).

It was chosen for its nice property of having metadata retaining all packets acknowledged by a peer being redundantly distributed across all packets, which significantly reduces chances for packet loss.

**reliable** also includes the additional following features from [networkprotocol/reliable.io](https://github.com/networkprotocol/reliable.io):

1. Packets being handled by the reliability layer that have yet to be acknowledged are re-transmitted every 100ms.
2. A flag may be set on packets to optionally avoid being handled by the reliability layer.
3. Packets stop being sent when it is suspected the recipients read buffer is full.
4. Packet acknowledgements are automatically sent when too many are buffered up.

** This project is still a WIP! Scrub through the _FIXME_ and _TODO_ comments in the source code, or open up a Github issue if you would like to help out!

## Rationale

On my quest for finding a feasible solution against TCP head-of-line blocking, I looked through a _lot_ of reliable UDP libraries in Go, with the majority primarily suited for either file transfer or gaming:

1. [jakecoffman/rely](https://github.com/jakecoffman/rely)
2. [obsilp/rmnp](https://github.com/obsilp/rmnp)
3. [xtaci/kcp-go](https://github.com/xtaci/kcp-go)
4. [ooclab/es](https://github.com/ooclab/es/tree/master/proto/udp)
5. [arl/udpnet](https://github.com/arl/udpnet)
6. [warjiang/utp](https://github.com/warjiang/utp/tree/master/utp)
7. [go-guoyk/sptp](https://github.com/go-guoyk/sptp)
8. [spance/suft](https://github.com/spance/suft/)

I felt that they did _just_ a little too much, as all I wanted was a modular reliability layer that I can plug into my existing UDP-based networking protocol.

After all, getting the reliability layer of a protocol right and performant is hard, and honestly something you only want to have to ever worry about once.

As a result, I started crufting up some time to work on creating **reliable**.

## Features

1. Uses only one goroutine per [`net.PacketConn`](https://golang.org/pkg/net/#PacketConn), and one goroutine per incoming client.
2. Byte buffer pools used for the protocol are instantiated per [`net.PacketConn`](https://golang.org/pkg/net/#PacketConn). You may pass in your own [byte buffer pool](https://github.com/valyala/bytebufferpool) as well.
3. Minimal overhead of at most 9 bytes per packet.

## What's missing?

1. Packet fragmentation/reassembly is still missing. Packets must be reasonably kept in terms of size under MTU (~1400 bytes).
2. Reduce locking/use of channels in as many code hot paths as possible.
3. Networking statistics (packet loss, RTT, etc.).

## Usage

```
$ go get github.com/lithdew/reliable
```

## Example

You may run the example below by executing the following command:

```
$ go run github.com/lithdew/reliable/examples/basic
```

```go
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
```