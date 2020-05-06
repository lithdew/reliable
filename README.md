# reliable

[![MIT License](https://img.shields.io/apm/l/atomic-design-ui.svg?)](LICENSE)
[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lithdew/reliable)
[![Discord Chat](https://img.shields.io/discord/697002823123992617)](https://discord.gg/HZEbkeQ)

**reliable** is a reliability layer for UDP connections in Go.

With only 9 bytes of packet overhead at most, what **reliable** does for your UDP-based application is:

1. handle acknowledgement over the recipient of packets you sent,
2. handle sending acknowledgements when too many are being buffered up,
3. handle resending sent packets whose recipient hasn't been acknowledged after some timeout, and
4. handle stopping/buffering up packets to be sent when the recipients read buffer is suspected to be full.

** This project is still a WIP! Scrub through the source code, write some unit tests, help out with documentation, or open up a Github issue if you would like to help out or have any questions!

## Protocol

### Packet Header

**reliable** uses the same packet header layout described in [`networkprotocol/reliable.io`](https://github.com/networkprotocol/reliable.io).

All packets start with a single byte (8 bits) representing 8 different flags. Packets are sequential, and are numbered using an unsigned 16-bit integer included in the packet header unless the packet is marked to be unreliable.

Packet acknowledgements (ACKs) are redundantly included in every sent packet using a total of 5 bytes: two bytes representing an unsigned 16-bit packet sequence number (ack), and three bytes representing a 32-bit bitfield (ackBits).

The packet header layout, much like [`networkprotocol/reliable.io`](https://github.com/networkprotocol/reliable.io), is delta-encoded and RLE-encoded to reduce the size overhead per packet.

### Packet Acknowledgements

Given a packet we have just received from our peer, for each set bit (i) in the bitfield (ackBits), we mark a packet we have sent to be acknowledged if its sequence number is (ack - i).

In the case of peer A sending packets to B, with B not sending any packets at all to A, B will send an empty packet for every 32 packets received from A so that A will be aware that B has acknowledged its packets.

More explicitly, a counter (lui) is maintained representing the last consecutive packet sequence number that we have received whose acknowledgement we have told to our peer about.

For example, if (lui) is 0, and we have sent acknowledgements for packets whose sequence numbers are 2, 3, 4, and 6, and we have then acknowledged packet sequence number 1, then lui would be 4.

Upon updating (lui), if the next 32 consecutive sequence numbers are sequence numbers of packets we have previously received, we will increment (lui) by 32 and send a single empty packet containing the following packet acknowledgements: (ack=lui+31, ackBits=[lui,lui+31]).

### Packet Buffering

Two fixed-sized sequence buffers are maintained for packets that we have sent (wq), and packets that we have received (rq). The size fixed for these buffers must evenly divide into the max value of an unsigned 16-bit integer (65536). The data structure is described in [this blog post by Glenn Fiedler](https://gafferongames.com/post/reliable_ordered_messages/).

We keep track of a counter (oui), representing the last consecutive sequence number of a packet we have sent that was acknowledged by our peer. For example, if we have sent packets whose sequence numbers are in the range [0, 256], and we have received acknowledgements for packets (0, 1, 2, 3, 4, 8, 9, 10, 11, 12), then (oui) would be 4.

Let cap(q) be the fixed size or capacity of sequence buffer q.

While sending packets, we intermittently stop and buffer the sending of packets if we believe sending more packets would overflow the read buffer of our recipient. More explicitly, if the next packet we sent is assigned a packet number greater than (oui + cap(rq)), we stop all sends until (oui) has incremented through the recipient of a packet from our peer.

### Retransmitting Lost Packets

The logic for retransmitting stale, unacknowledged sent packets and maintaining acknowledgements was taken with credit to [this blog post by Glenn Fiedler](https://gafferongames.com/post/reliable_ordered_messages/).

Packets are suspected to be lost if they are not acknowledged by their recipient after 100ms. Once a packet is suspected to be lost, it is resent. As of right now, packets are resent for a maximum of 10 times.

It might be wise to not allow packets to be resent a capped number of times, and to leave it up to the developer. However, that is open for discussion which I am happy to have over on my Discord server or through a Github issue.

## Rationale

On my quest for finding a feasible solution against TCP head-of-line blocking, I looked through a _lot_ of reliable UDP libraries in Go, with the majority primarily suited for either file transfer or gaming:

1. A direct port of the reference C implementation of [networkprotocol/reliable.io](https://github.com/networkprotocol/reliable.io): [jakecoffman/rely](https://github.com/jakecoffman/rely)
2. A realtime multiplayer network gaming protocol: [obsilp/rmnp](https://github.com/obsilp/rmnp)
3. A reliable, production-grade ARQ protocol: [xtaci/kcp-go](https://github.com/xtaci/kcp-go)
4. An encrypted session-based streaming protocol: [ooclab/es](https://github.com/ooclab/es/tree/master/proto/udp)
5. A game networking protocol: [arl/udpnet](https://github.com/arl/udpnet)
6. A direct port of uTP (Micro Transport Protocol): [warjiang/utp](https://github.com/warjiang/utp/tree/master/utp)
7. A protocol for sending arbitrarily large, chunked amounts of data: [go-guoyk/sptp](https://github.com/go-guoyk/sptp)
8. A small-scale fast transmission protocol: [spance/suft](https://github.com/spance/suft/)
9. A direct port of QUIC: [lucas-clemente/quic-go](https://github.com/lucas-clemente/quic-go)

Going through all of them, I felt that they did just a little too much for me. For my work and side projects, I have been working heavily on decentralized p2p networking protocols. The nature of these protocols is that they suffer heavily from TCP head-of-line blocking operating in high-latency/high packet loss environments.

In many cases, a lot of the features provided by these libraries were either not needed, or honestly felt like they would best be handled and thought through by the developer using these libraries. For example:
 
1. handshaking/session management
2. packet fragmentation/reassembly
3. packet encryption/decryption

So, I began working on a modular approach and decided to abstract away the reliability portion of protocols I have built into a separate library.

I feel that this approach is best versus the popular alternatives like QUIC or SCTP that may, depending on your circumstances, do just a bit too much for you. After all, getting _just_ the reliability bits of a UDP-based protocol correct and well-tested is hard enough.

## Todo

1. Estimate the round-trip time (RTT) and adjust the system's packet re-transmission delay based off of it.
2. Encapsulate away protocol logic and `net.PacketConn`-related bits for a finer abstraction.
3. Keep a cache of the string representations of passed-in `net.UDPAddr`.
4. Reduce locking in as many code hot paths as possible.
5. Networking statistics (packet loss, RTT, etc.).
6. More unit tests.

## Usage

**reliable** uses Go modules. To include it in your project, run the following command:

```
$ go get github.com/lithdew/reliable
```

Should you just be looking to quickly get a project or demo up and running, use `Endpoint`. If you require more flexibility, consider directly working with `Conn`.

Note that some sort of keep-alive mechanism or heartbeat system needs to be bootstrapped on top, otherwise packets may indefinitely be resent as they will have failed to be acknowledged. 

## Options

1. The read buffer size may be configured using `WithReadBufferSize`. The default read buffer size is 256.
2. The write buffer size may be configured using `WithWriteBufferSize`. The default write buffer size is 256.
3. The minimum period of time before we retransmit an packet that has yet to be acknowledged may be configured using `WithResendTimeout`. The default resend timeout is 100 milliseconds.
4. A packet handler which is to be called back when a packet is received may be configured using `WithPacketHandler`. By default, a nil handler is provided which ignores all incoming packets.
5. An error handler which is called when errors occur on a connection that may be configured using `WithErrorHandler` By default, a nil handler is provided which ignores all errors.
6. A byte buffer pool may be passed in using `WithBufferPool`. By default, a new byte buffer pool is instantiated.

## Benchmarks

A benchmark was done using [`cmd/benchmark`](examples/benchmark) from Japan to a DigitalOcean 2GB / 60 GB Disk / NYC3 server.

The benchmark task was to spam 1400 byte packets from Japan to New York. Given a ping latency of roughly 220 milliseconds, the throughput was roughly 1.2 MiB/sec.

Unit test benchmarks have also been performed, as shown below.

```
$ cat /proc/cpuinfo | grep 'model name' | uniq
model name : Intel(R) Core(TM) i7-7700HQ CPU @ 2.80GHz

$ go test -bench=. -benchtime=10s
goos: linux
goarch: amd64
pkg: github.com/lithdew/reliable
BenchmarkEndpointWriteReliablePacket-8           2053717              5941 ns/op             183 B/op          9 allocs/op
BenchmarkEndpointWriteUnreliablePacket-8         2472392              4866 ns/op             176 B/op          8 allocs/op
BenchmarkMarshalPacketHeader-8                  749060137               15.7 ns/op             0 B/op          0 allocs/op
BenchmarkUnmarshalPacketHeader-8                835547473               14.6 ns/op             0 B/op          0 allocs/op
```

## Example

You may run the example below by executing the following command:

```
$ go run github.com/lithdew/reliable/examples/basic
```

This example demonstrates:

1. how to quickly construct two UDP endpoints listening on ports 44444 and 55555, and
2. how to have the UDP endpoint at port 44444 spam 1400-byte packets to the UDP endpoint at port 55555 as fast as possible.

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
	PacketData = bytes.Repeat([]byte("x"), 1400)
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

	a := reliable.NewEndpoint(ca, reliable.WithPacketHandler(handler))
	b := reliable.NewEndpoint(cb, reliable.WithPacketHandler(handler))

	defer func() {
		check(ca.SetDeadline(time.Now().Add(1 * time.Millisecond)))
		check(cb.SetDeadline(time.Now().Add(1 * time.Millisecond)))

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
```