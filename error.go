package reliable

import (
	"errors"
	"io"
	"net"
)

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
