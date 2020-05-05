package reliable

import (
	"sync"
)

type workItem struct {
	buf  []byte
	done chan error
}

func (c *workItem) finish(err error) {
	c.done <- err
}

var workItemPool sync.Pool

func acquireWorkItem() *workItem {
	w := workItemPool.Get()
	if w == nil {
		w = &workItem{done: make(chan error, 1)}
	}
	return w.(*workItem)
}

func releaseWorkItem(w *workItem) {
	w.buf = nil
	workItemPool.Put(w)
}
