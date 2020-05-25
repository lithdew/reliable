package reliable

import (
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"sync"
	"testing"
)

func testConnWaitForWriteDetails(inc uint16) func(t testing.TB) {
	return func(t testing.TB) {
		defer goleak.VerifyNone(t)

		p := NewProtocol()
		p.wi = uint16(len(p.rq))

		var wg sync.WaitGroup
		wg.Add(8)

		ch := make(chan uint16, 8)

		for i := 0; i < 8; i++ {
			go func() {
				defer wg.Done()

				idx, _, _, _ := p.waitForNextWriteDetails()
				ch <- idx
			}()
		}

		for i := 0; i < 8; i++ {
			p.ouc.L.Lock()
			p.oui += inc
			p.ouc.Broadcast()
			p.ouc.L.Unlock()
		}

		wg.Wait()

		expected := make(map[uint16]struct{}, 8)

		close(ch)
		for idx := range ch {
			expected[idx] = struct{}{}
		}

		for i := 0; i < 8; i++ {
			actual := uint16(len(p.wq) + i)
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
