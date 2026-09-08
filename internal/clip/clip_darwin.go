//go:build darwin

package clip

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

long getChangeCount() {
    return [[NSPasteboard generalPasteboard] changeCount];
}

const char* getString() {
    NSString *s = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString];
    if (s == nil) return NULL;
    return [s UTF8String];
}

void setString(const char* str) {
    NSPasteboard *pb = [NSPasteboard generalPasteboard];
    [pb clearContents];
    NSString *ns = [NSString stringWithUTF8String:str];
    [pb setString:ns forType:NSPasteboardTypeString];
}
*/
import "C"
import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/oklog/ulid/v2"
)

var suppress atomic.Bool

func watch(ctx context.Context) (<-chan store.Item, error) {
	ch := make(chan store.Item, 4)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		last := int64(C.getChangeCount())
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cc := int64(C.getChangeCount())
				if cc == last {
					continue
				}
				last = cc
				if suppress.Load() {
					suppress.Store(false)
					continue
				}
				cstr := C.getString()
				if cstr == nil {
					continue
				}
				goStr := C.GoString(cstr)
				if goStr == "" {
					continue
				}
				data := []byte(goStr)
				h := sha256.Sum256(data)
				sha := fmt.Sprintf("%x", h[:])
				it := store.Item{
					ID:      ulid.Make().String(),
					Kind:    "text",
					Mime:    "text/plain; charset=utf-8",
					Size:    int64(len(data)),
					SHA256:  sha,
					Created: time.Now(),
					Inline:  data,
				}
				select {
				case ch <- it:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

func set(it store.Item) error {
	if it.Kind != "text" {
		return fmt.Errorf("only text supported in this build")
	}
	suppress.Store(true)
	cstr := C.CString(string(it.Inline))
	defer C.free(unsafe.Pointer(cstr))
	C.setString(cstr)
	return nil
}
