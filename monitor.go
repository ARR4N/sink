package sink

import (
	"context"
	"slices"
)

// A Monitor is a mutual exclusion lock, guarding a specific value, with the
// added ability to wait until a condition predicated on the value is met. It is
// intended as an alternative to `sync.Cond`.
//
// The zero value of a Monitor is invalid. It is safe to copy a Monitor.
type Monitor[T any] struct {
	// A Monitor achieves mutual exclusion in the same way as [Mutex] but guards
	// both the value and a set of "waiters".
	ch chan monitorState[T]
}

type monitorState[T any] struct {
	v       T
	waiters []*waiter[T]
}

type waiter[T any] struct {
	cond func(T) bool
	ch   chan T
}

// NewMonitor creates a new Monitor and sets the initial value to `init`. Call
// [Monitor.Close] to release resources.
func NewMonitor[T any](init T) Monitor[T] {
	m := Monitor[T]{
		ch: make(chan monitorState[T], 1),
	}
	m.ch <- monitorState[T]{v: init}
	return m
}

// Wait calls `cond` with the guarded value, one or more times, until it returns
// true, after which it calls `fn` with the guarded value. Repeated calls to
// `cond` will be blocked until signalled via a concurrent call to
// [Monitor.UseThenSignal].
func (m Monitor[T]) Wait(ctx context.Context, cond func(T) bool, fn ExclusiveAccess[T]) error {
	select {
	case <-ctx.Done():
		return ctx.Err()

	case state, ok := <-m.ch:
		if !ok {
			return closedErr[T, Monitor[T]]{}
		}
		if cond(state.v) {
			err := fn(state.v)
			m.ch <- state
			return err
		}

		// `ch` is effectively a [Mutex] to be shared between us and the
		// signaller. It has a buffer of 1 in case we stop waiting, in which
		// case the signaller immediately receives the value back. `ch` will be
		// closed by either the signaller or [Monitor.Close].
		ch := make(chan T, 1)
		state.waiters = append(state.waiters, &waiter[T]{
			cond: cond,
			ch:   ch,
		})

		// We now mirror the behaviour of `sync.Cond.Wait()`, which "atomically
		// unlocks c.L and suspends execution of the calling goroutine".
		//
		// unlock
		m.ch <- state
		// suspend
		select {
		case <-ctx.Done():
			return ctx.Err()
		case v, ok := <-ch:
			if !ok {
				return closedErr[T, Monitor[T]]{}
			}
			err := fn(v)
			ch <- v
			return err
		}
	}
}

// UseThenSignal calls `fn` with the guarded value, returning immediately if
// `fn` returns an error (which is propagated). Otherwise, the conditions of all
// goroutines blocked by [Monitor.Wait] are checked and those that return true
// receive the guarded value for their own, respective [ExclusiveAccess] functions.
func (m Monitor[T]) UseThenSignal(ctx context.Context, fn ExclusiveAccess[T]) error {
	select {
	case <-ctx.Done():
		return ctx.Err()

	case state, ok := <-m.ch:
		if !ok {
			return closedErr[T, Monitor[T]]{}
		}
		defer func() { m.ch <- state }()

		if err := fn(state.v); err != nil {
			return err
		}

		for i, w := range state.waiters {
			if !w.cond(state.v) {
				continue
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case w.ch <- state.v:
				_ = 0 // coverage visual aid
			}
			// Either the waiter is finished with the value, or it had stopped
			// waiting so the value was in the buffer.
			<-w.ch
			close(w.ch)
			state.waiters[i] = nil
		}

		state.waiters = slices.DeleteFunc(state.waiters, func(w *waiter[T]) bool {
			return w == nil
		})
		return nil
	}
}

// Close releases the Monitors's resources. Any future calls to
// [Monitor.UseThenSignal] or [Monitor.Wait] will return [ErrClosed].
// [ErrClosed]. Close returns the guraded value.
func (m Monitor[T]) Close() T {
	s := <-m.ch
	close(m.ch)
	for _, w := range s.waiters {
		close(w.ch)
	}
	return s.v
}

// FromMonitor is a convenience wrapper around [Monitor.Wait], returning a value
// derived from the guarded value.
func FromMonitor[T any, U any](ctx context.Context, mon Monitor[T], cond func(T) bool, fn ExclusiveAccessValuer[T, U]) (U, error) {
	var u U
	err := mon.Wait(ctx,
		cond,
		func(t T) error {
			var err error
			u, err = fn(t)
			return err
		},
	)
	return u, err
}

// FromMonitors is a convenience wrapper around nested calls to [FromMonitor],
// returning a value derived from both guarded values. Waiting is performed in
// the same order as the condition arguments, so `m0` is locked the longest.
func FromMonitors[T any, U any, V any](
	ctx context.Context,
	m0 Monitor[T], m1 Monitor[U],
	c0 func(T) bool, c1 func(U) bool,
	fn ExclusiveMultiAccessValuer[T, U, V],
) (V, error) {
	return FromMonitor(ctx, m0, c0, func(t T) (V, error) {
		return FromMonitor(ctx, m1, c1, func(u U) (V, error) {
			return fn(t, u)
		})
	})
}
