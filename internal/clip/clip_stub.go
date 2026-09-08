//go:build !darwin && !windows

package clip

import (
	"context"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
)

var memClip store.Item

func watch(ctx context.Context) (<-chan store.Item, error) {
	ch := make(chan store.Item)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func set(it store.Item) error {
	memClip = it
	return nil
}

// Get returns last set item (for tests).
func Get() store.Item { return memClip }
