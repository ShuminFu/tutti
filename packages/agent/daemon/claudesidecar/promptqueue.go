package claudesidecar

import (
	"errors"
	"sync"
)

// asyncPromptQueue mirrors promptQueue.ts: an unbounded FIFO of user messages
// with a blocking iterator and a terminal close.
type asyncPromptQueue struct {
	mu     sync.Mutex
	values []map[string]any
	wake   chan struct{}
	closed bool
}

func newAsyncPromptQueue() *asyncPromptQueue {
	return &asyncPromptQueue{wake: make(chan struct{}, 1)}
}

func (q *asyncPromptQueue) push(message map[string]any) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return errors.New("prompt queue is closed")
	}
	q.values = append(q.values, message)
	q.mu.Unlock()
	q.signal()
	return nil
}

func (q *asyncPromptQueue) close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.mu.Unlock()
	q.signal()
}

func (q *asyncPromptQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// next blocks until a message is available or the queue is closed and
// drained; ok=false signals the terminal close.
func (q *asyncPromptQueue) next() (map[string]any, bool) {
	for {
		q.mu.Lock()
		if len(q.values) > 0 {
			message := q.values[0]
			q.values = q.values[1:]
			q.mu.Unlock()
			return message, true
		}
		if q.closed {
			q.mu.Unlock()
			return nil, false
		}
		q.mu.Unlock()
		<-q.wake
	}
}
