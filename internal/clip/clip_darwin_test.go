//go:build darwin

package clip

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
)

func TestDarwinSetSuppress(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("no window server in CI")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	// Set should not cause Watch to emit (suppress)
	it := store.Item{Kind: "text", Inline: []byte("hello-darwin-race")}
	if err := Set(it); err != nil {
		t.Fatalf("Set: %v", err)
	}
	select {
	case got := <-ch:
		t.Fatalf("expected no emit after Set, got %v", got)
	case <-time.After(700 * time.Millisecond):
		// ok, no echo within 600ms
	}
}

func TestDarwinSetConcurrentRace(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("no window server in CI")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _ := Watch(ctx)
	// Concurrent Set and watcher read
	go func() {
		for i := 0; i < 10; i++ {
			_ = Set(store.Item{Kind: "text", Inline: []byte("race")})
			time.Sleep(10 * time.Millisecond)
		}
	}()
	// Drain ch for a bit to exercise race
	timeout := time.After(300 * time.Millisecond)
	for {
		select {
		case <-ch:
		case <-timeout:
			return
		}
	}
}
