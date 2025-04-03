package sink

import (
	"context"
	"errors"
	"sync"
	"testing"

	"golang.org/x/sync/errgroup"
)

// TODO(arr4n) add a test using the "synctest" package.

func TestMutex(t *testing.T) {
	ctx := context.Background()
	const (
		begin = 42
		n     = 100
	)
	muToCopy := NewMutex(begin)
	mu := muToCopy

	start := make(chan struct{})
	var g errgroup.Group
	for range n {
		g.Go(func() error {
			<-start
			return mu.Replace(ctx, func(i int) (int, error) {
				return i + 1, nil
			})
		})
	}
	close(start)
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}

	want := begin + n
	t.Run("Use", func(t *testing.T) {
		err := mu.Use(ctx, func(got int) error {
			if got != want {
				t.Errorf("got %d; want %d", got, want)
			}
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("Close", func(t *testing.T) {
		if got := mu.Close(); got != want {
			t.Errorf("%T.Close() got %d; want %d (final value)", mu, got, want)
		}
		if err, ok := mu.Use(ctx, nil).(closedErr[int, Mutex[int]]); !ok {
			t.Errorf("%T.Use() after Close(); got %v; want %v", mu, err, closedErr[int, Mutex[int]]{})
		}
		if err, ok := mu.Replace(ctx, nil).(closedErr[int, Mutex[int]]); !ok {
			t.Errorf("%T.Replace() after Close() got %v; want %v", mu, err, closedErr[int, Mutex[int]]{})
		}
	})
}

func TestFromMutex(t *testing.T) {
	ctx := context.Background()
	const initial = 42
	mu := NewMutex(initial)

	got, err := FromMutex(ctx, mu, func(i int) (int, error) {
		return i + 1, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := initial + 1; got != want {
		t.Errorf("got %d; want %d", got, want)
	}
}

func TestMutexContextAwareness(t *testing.T) {
	mu := NewMutex(0)

	ready := make(chan struct{})
	quit := make(chan struct{})
	done := make(chan error)
	go func() {
		done <- mu.Replace(context.Background(), func(int) (int, error) {
			close(ready)
			<-quit
			return 0, nil
		})
	}()

	<-ready
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FromMutex[int, struct{}](ctx, mu, nil); !errors.Is(err, context.Canceled) {
		t.Error(err)
	}
	if err := mu.Replace(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Error(err)
	}

	close(quit)
	if err := <-done; err != nil {
		t.Error(err)
	}
	close(done)
}

func BenchmarkMutex(b *testing.B) {
	// TL;DR [Mutex] is slower than [sync.Mutex] although measured in 10s of
	// nanoseconds on a 2016 laptop. The performance hit is the price for being
	// hard to misuse.

	// goos: linux
	// goarch: amd64
	// pkg: github.com/arr4n/sink
	// cpu: Intel(R) Core(TM) i7-7500U CPU @ 2.70GHz
	//
	// BenchmarkMutex/sink-4           14899579                70.76 ns/op
	// BenchmarkMutex/sync-4           96168532                12.33 ns/op
	//
	// BenchmarkMutex/sink-4           13888718                84.17 ns/op            0 B/op          0 allocs/op
	// BenchmarkMutex/sync-4           97091973                12.34 ns/op            0 B/op          0 allocs/op

	b.Run("sink", func(b *testing.B) {
		ctx := context.Background()
		mu := NewMutex(0)

		b.ResetTimer()
		for range b.N {
			mu.Replace(ctx, func(i int) (int, error) {
				return i + 1, nil
			})
		}
	})

	b.Run("sync", func(b *testing.B) {
		var (
			i  int
			mu sync.Mutex
		)

		b.ResetTimer()
		for range b.N {
			mu.Lock()
			i++
			mu.Unlock()
		}
	})
}
