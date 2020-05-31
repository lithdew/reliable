package reliable

import (
	"github.com/stretchr/testify/require"
	"net"
	"testing"
	"time"
)

func TestConnApply(t *testing.T) {
	a, _ := net.ListenPacket("udp", "127.0.0.1:0")
	b, _ := net.ListenPacket("udp", "127.0.0.1:0")

	ca := NewConn(b.LocalAddr(), a)

	expectedUpdatePeriod := 1 * time.Second
	expectedResendTimeout := 2 * time.Second

	uOpt := WithUpdatePeriod(expectedUpdatePeriod)
	rOpt := WithResendTimeout(expectedResendTimeout)

	uOpt.applyConn(ca)
	require.EqualValues(t, expectedUpdatePeriod, ca.updatePeriod)

	rOpt.applyConn(ca)
	require.EqualValues(t, expectedResendTimeout, ca.resendTimeout)
}
