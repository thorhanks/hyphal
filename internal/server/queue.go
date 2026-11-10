package server

import (
	"context"
	"sync"
)

// queue is one tab's send queue, so a slow tab never blocks the session
// goroutine (ARCHITECTURE §5). A tab that falls sendBuffer messages behind is
// disconnected rather than sent a partial stream.
type queue struct {
	mu       sync.Mutex
	items    [][]byte
	overflow bool
	notify   chan struct{}
}

func newQueue() *queue { return &queue{notify: make(chan struct{}, 1)} }

func (q *queue) push(b []byte) {
	q.mu.Lock()
	if len(q.items) >= sendBuffer {
		q.overflow = true // TODO(P1-30): resync the tab with one fresh state instead
	} else {
		q.items = append(q.items, b)
	}
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (q *queue) pop(ctx context.Context) ([]byte, bool) {
	for {
		q.mu.Lock()
		if q.overflow {
			q.mu.Unlock()
			return nil, false
		}
		if len(q.items) > 0 {
			b := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return b, true
		}
		q.mu.Unlock()
		select {
		case <-q.notify:
		case <-ctx.Done():
			return nil, false
		}
	}
}
