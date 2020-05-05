package main

import (
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"sync"
	"testing"
)

func testConnWaitForWriteDetails(inc uint16) func(t testing.TB) {
	return func(t testing.TB) {
		defer goleak.VerifyNone(t)

		c := NewConn(nil, nil, nil)
		c.wi = uint16(len(c.rq))

		var wg sync.WaitGroup
		wg.Add(8)

		ch := make(chan uint16, 8)

		for i := 0; i < 8; i++ {
			go func() {
				defer wg.Done()

				idx, _, _, _ := c.waitForNextWriteDetails()
				ch <- idx
			}()
		}

		for i := 0; i < 8; i++ {
			c.ouc.L.Lock()
			c.oui += inc
			c.ouc.Broadcast()
			c.ouc.L.Unlock()
		}

		wg.Wait()

		expected := make(map[uint16]struct{}, 8)

		close(ch)
		for idx := range ch {
			expected[idx] = struct{}{}
		}

		for i := 0; i < 8; i++ {
			actual := uint16(len(c.wq) + i)
			require.Contains(t, expected, actual)
			delete(expected, actual)
		}
	}
}

func TestConnWaitForWriteDetails(t *testing.T) {
	testConnWaitForWriteDetails(1)(t)
	testConnWaitForWriteDetails(2)(t)
	testConnWaitForWriteDetails(4)(t)
}
