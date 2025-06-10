package sink

import (
	"context"
)

// A Gate blocks waiters until it is opened. Unlike a closed channel, it can be
// reused.
type Gate struct {
	// The mutex channel holds another channel which, if non-nil, acts as the
	// blocking channel to be closed.
	mutex chan chan struct{}
}

// NewGate constructs a new, open. Gate.
func NewGate() Gate {
	mutex := make(chan chan struct{}, 1)
	mutex <- nil // no initial blocker
	return Gate{mutex}
}

// Open unblocks all goroutines blocked on [Gate.Wait]. Repeated calls are
// idempotent.
func (g Gate) Open() {
	if blocker := <-g.mutex; blocker != nil {
		close(blocker)
	}
	g.mutex <- nil
}

// Block blocks all future calls to [Gate.Wait] until [Gate.Open] is called.
// Repeated calls are idempotent.
func (g Gate) Block() {
	blocker := <-g.mutex
	if blocker == nil {
		blocker = make(chan struct{})
	}
	g.mutex <- blocker
}

// Wait blocks until the Gate is opened or the context is cancelled.
func (g Gate) Wait(ctx context.Context) error {
	blocker := <-g.mutex
	g.mutex <- blocker

	if blocker == nil {
		return nil
	}

	select {
	case <-blocker:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
