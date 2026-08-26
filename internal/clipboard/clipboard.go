// Package clipboard is a minimal Win32 clipboard wrapper: enough to place
// Unicode text on the clipboard (used by visual-mode yank).
package clipboard

import (
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	clipRetries   = 3
	clipDelay     = 10 * time.Millisecond
)

var (
	user32       = syscall.NewLazyDLL("user32.dll")
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	openClip     = user32.NewProc("OpenClipboard")
	closeClip    = user32.NewProc("CloseClipboard")
	emptyClip    = user32.NewProc("EmptyClipboard")
	setClipData  = user32.NewProc("SetClipboardData")
	getClipData  = user32.NewProc("GetClipboardData")
	globalAlloc  = kernel32.NewProc("GlobalAlloc")
	globalLock   = kernel32.NewProc("GlobalLock")
	globalUnlock = kernel32.NewProc("GlobalUnlock")
	globalFree   = kernel32.NewProc("GlobalFree")
	globalSize   = kernel32.NewProc("GlobalSize")
	copyMem      = kernel32.NewProc("RtlMoveMemory")
)

// openClipboard opens the clipboard with a short retry loop. The clipboard
// can be briefly locked by another process performing a paste or copy.
func openClipboard() error {
	var r uintptr
	for i := 0; i < clipRetries; i++ {
		r, _, _ = openClip.Call(0)
		if r != 0 {
			return nil
		}
		time.Sleep(clipDelay)
	}
	return syscall.GetLastError()
}

// SetText replaces the clipboard contents with the given Unicode text.
func SetText(text string) error {
	if text == "" {
		text = "\r\n"
	}
	if err := openClipboard(); err != nil {
		return err
	}
	defer func() { _, _, _ = closeClip.Call() }()

	if r, _, _ := emptyClip.Call(); r == 0 {
		return syscall.GetLastError()
	}

	// CF_UNICODETEXT expects a UTF-16LE buffer with a trailing NUL.
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := uintptr(len(utf16) * 2)
	mem, _, _ := globalAlloc.Call(gmemMoveable, size)
	if mem == 0 {
		return syscall.GetLastError()
	}

	ptr, _, _ := globalLock.Call(mem)
	if ptr == 0 {
		_, _, _ = globalFree.Call(mem)
		return syscall.GetLastError()
	}
	// Copy through the Win32 API so no unsafe.Pointer conversions of the
	// locked memory are needed.
	_, _, _ = copyMem.Call(ptr, uintptr(unsafe.Pointer(&utf16[0])), size)
	_, _, _ = globalUnlock.Call(mem)

	// On success the system owns the memory; only free it on failure.
	if r, _, _ := setClipData.Call(cfUnicodeText, mem); r == 0 {
		_, _, _ = globalFree.Call(mem)
		return syscall.GetLastError()
	}
	return nil
}

// GetText returns the current Unicode text from the clipboard, or an error
// if the clipboard is empty or holds no text.
func GetText() (string, error) {
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer func() { _, _, _ = closeClip.Call() }()

	mem, _, _ := getClipData.Call(cfUnicodeText)
	if mem == 0 {
		return "", syscall.GetLastError()
	}
	ptr, _, _ := globalLock.Call(mem)
	if ptr == 0 {
		return "", syscall.GetLastError()
	}
	defer func() { _, _, _ = globalUnlock.Call(mem) }()

	// Read the block into a Go buffer, then trim at the NUL terminator.
	size, _, _ := globalSize.Call(mem)
	n := int(size / 2)
	if n <= 0 {
		return "", nil
	}
	buf := make([]uint16, n)
	// Copy at most n*2 bytes: a malformed odd-sized clipboard block would
	// otherwise write one byte past the backing array.
	copySize := uintptr(n * 2)
	_, _, _ = copyMem.Call(uintptr(unsafe.Pointer(&buf[0])), ptr, copySize)

	end := 0
	for end < len(buf) && buf[end] != 0 {
		end++
	}
	return string(utf16.Decode(buf[:end])), nil
}
