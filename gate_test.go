package sink

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGate(t *testing.T) {
	t.Parallel()

	// TODO(arr4n) use synctest once the Go version is bumped.
	timeoutCtx := func(d time.Duration) context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		t.Cleanup(cancel)
		return ctx
	}
	shortCtx := func() context.Context {
		return timeoutCtx(20 * time.Millisecond)
	}
	longCtx := func() context.Context {
		return timeoutCtx(100 * time.Millisecond)
	}

	_ = []any{shortCtx, longCtx}

	g := NewGate()

	assertWait := func(ctx context.Context, t *testing.T, want error, format string, a ...any) {
		t.Helper()
		if err := g.Wait(ctx); !errors.Is(err, want) {
			t.Errorf("%s: %T.Wait() got %v; want %v", fmt.Sprintf(format, a...), g, err, want)
		}
	}
	assertOpen := func(t *testing.T, format string, a ...any) {
		t.Helper()
		assertWait(shortCtx(), t, nil, format, a...)
	}
	assertBlocked := func(t *testing.T, format string, a ...any) {
		t.Helper()
		assertWait(longCtx(), t, context.DeadlineExceeded, format, a...)
	}

	assertOpen(t, "new %T", g)
	g.Block()
	assertBlocked(t, "after %T.Block()", g)
	g.Open()
	assertOpen(t, "after %T.Block() then %[1]T.Open()", g)

	t.Run("multiple_waiters", func(t *testing.T) {
		g.Block()

		var (
			ready, finished sync.WaitGroup
			released        atomic.Uint64
		)
		for range 10 {
			ready.Add(1)
			finished.Add(1)

			go func() {
				ready.Done()
				g.Wait(context.Background())
				released.Add(1)
				finished.Done()
			}()
		}

		ready.Wait()
		if got := released.Load(); got != 0 {
			t.Fatalf("Before %T.Open(); got %d released, want 0", g, got)
		}

		g.Open()
		finished.Wait()
	})

	t.Run("high_concurrency", func(t *testing.T) {
		g.Block()

		var wg sync.WaitGroup
		start := make(chan struct{})

		rng := rand.New(rand.NewSource(0))
		for range 50_000 {
			wg.Add(1)
			open := rng.Intn(2) == 0
			go func() {
				<-start
				if open {
					g.Open()
				} else {
					g.Block()
				}
				wg.Done()
			}()
		}

		var waiters sync.WaitGroup
		for range 50_000 {
			waiters.Add(1)
			go func() {
				g.Wait(context.Background())
				waiters.Done()
			}()
		}

		close(start)
		wg.Wait()
		g.Open()
		waiters.Wait()
	})
}
