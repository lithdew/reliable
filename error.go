package reliable

import (
	"errors"
	"io"
	"log"
	"net"
)

func check(err error) {
	if err != nil {
		if isEOF(err) {
			return
		}
		log.Panic(err)
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
