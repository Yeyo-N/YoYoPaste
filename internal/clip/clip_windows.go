//go:build windows

package clip

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/oklog/ulid/v2"
	"golang.org/x/sys/windows"
)

var (
	user32                         = windows.NewLazySystemDLL("user32.dll")
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
)

// procAddClipboardFormatListener is reserved for the future event-driven
// implementation (YYP-010). Currently watch uses polling fallback.
var _ = windows.NewLazySystemDLL("user32.dll").NewProc("AddClipboardFormatListener")
var _ = windows.NewLazySystemDLL("user32.dll").NewProc("RemoveClipboardFormatListener")

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

var suppress atomic.Bool

func watch(ctx context.Context) (<-chan store.Item, error) {
	ch := make(chan store.Item, 4)
	go func() {
		defer close(ch)
		hwnd, err := createMessageWindow(ch, ctx) //nolint:staticcheck
		if err != nil {                           //nolint:staticcheck
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			var last uint32
			if r, _, _ := procGetClipboardSequenceNumber.Call(); r != 0 {
				last = uint32(r)
			}
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					r, _, _ := procGetClipboardSequenceNumber.Call()
					seq := uint32(r)
					if seq == last {
						continue
					}
					last = seq
					if suppress.Load() {
						suppress.Store(false)
						continue
					}
					it := readClipboardText()
					if it == nil {
						continue
					}
					select {
					case ch <- *it:
					case <-ctx.Done():
						return
					}
				}
			}
		}
		_ = hwnd
		<-ctx.Done()
	}()
	return ch, nil
}

func createMessageWindow(ch chan store.Item, ctx context.Context) (windows.Handle, error) { //nolint:staticcheck
	return 0, fmt.Errorf("not implemented: message window")
}

func readClipboardText() *store.Item {
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return nil
	}
	defer func() { _, _, _ = procCloseClipboard.Call() }()
	ret, _, _ = procGetClipboardData.Call(uintptr(cfUnicodeText))
	h := windows.Handle(ret)
	if h == 0 {
		return nil
	}
	ret, _, _ = procGlobalLock.Call(uintptr(h))
	ptr := ret
	if ptr == 0 {
		return nil
	}
	defer func() { _, _, _ = procGlobalUnlock.Call(uintptr(h)) }()
	// GlobalAlloc memory is not on the Go heap, so the GC cannot move it;
	// the uintptr→unsafe.Pointer conversion is safe here. This is the
	// standard Win32 clipboard pattern and every implementation uses it.
	s := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(ptr)))
	if s == "" {
		return nil
	}
	data := []byte(s)
	hash := sha256.Sum256(data)
	return &store.Item{
		ID:      ulid.Make().String(),
		Kind:    "text",
		Mime:    "text/plain; charset=utf-8",
		Size:    int64(len(data)),
		SHA256:  fmt.Sprintf("%x", hash[:]),
		Created: time.Now(),
		Inline:  data,
	}
}

func set(it store.Item) error {
	if it.Kind != "text" {
		return fmt.Errorf("only text supported")
	}
	suppress.Store(true)
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return fmt.Errorf("open clipboard failed")
	}
	defer func() { _, _, _ = procCloseClipboard.Call() }()
	ret, _, _ = procEmptyClipboard.Call()
	if ret == 0 {
		return fmt.Errorf("empty clipboard failed")
	}
	utf16, err := windows.UTF16FromString(string(it.Inline))
	if err != nil {
		return err
	}
	size := len(utf16) * 2
	ret, _, _ = procGlobalAlloc.Call(uintptr(gmemMoveable), uintptr(size))
	h := windows.Handle(ret)
	if h == 0 {
		return fmt.Errorf("global alloc failed")
	}
	ret, _, _ = procGlobalLock.Call(uintptr(h))
	ptr := ret
	if ptr == 0 {
		_, _, _ = procGlobalFree.Call(uintptr(h))
		return fmt.Errorf("global lock failed")
	}
	dst := (*[1 << 20]uint16)(unsafe.Pointer(ptr))[:len(utf16):len(utf16)]
	copy(dst, utf16)
	_, _, _ = procGlobalUnlock.Call(uintptr(h))
	ret, _, _ = procSetClipboardData.Call(uintptr(cfUnicodeText), uintptr(h))
	if ret == 0 {
		_, _, _ = procGlobalFree.Call(uintptr(h))
		return fmt.Errorf("set clipboard failed")
	}
	return nil
}

var _ = time.Now
