package console

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"vimterm/internal/keybind"
)

// Event types reported by the console input loop.
type Event interface{ isEvent() }

// KeyEvent reports a key press.
type KeyEvent struct {
	Key keybind.Key
}

// ResizeEvent reports that the host terminal changed size.
type ResizeEvent struct {
	Cols, Rows int
}

func (KeyEvent) isEvent()   {}
func (ResizeEvent) isEvent() {}

// INPUT_RECORD event types.
const (
	eventKey  = 0x0001
	eventMouse = 0x0002
	eventWindowBufferSize = 0x0004
)

// inputRecord mirrors the Win32 INPUT_RECORD structure.
type inputRecord struct {
	eventType uint16
	_         [2]byte
	union     [16]byte
}

// keyEventRecord mirrors KEY_EVENT_RECORD (16 bytes).
type keyEventRecord struct {
	keyDown         int32
	repeatCount     uint16
	virtualKeyCode  uint16
	virtualScanCode uint16
	unicodeChar     uint16
	controlKeyState uint32
}

// windowBufferSizeRecord mirrors WINDOW_BUFFER_SIZE_RECORD.
type windowBufferSizeRecord struct {
	size windows.Coord
}

const (
	enableProcessedInput    = 0x0001
	enableLineInput         = 0x0002
	enableEchoInput         = 0x0004
	enableWindowInput       = 0x0008
	enableMouseInput        = 0x0010
	enableExtendedFlags     = 0x0080
	enableVirtualTermInput  = 0x0200
	enableProcessedOutput   = 0x0001
	enableVirtualTermOutput = 0x0004
	disableNewlineAutoRet   = 0x0008
)

var (
	procReadConsoleInputW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReadConsoleInputW")
)

// Console wraps the Windows console: it puts the host terminal into raw mode,
// streams input events, and writes VT output.
type Console struct {
	in, out   windows.Handle
	origIn    uint32
	origOut   uint32
	events    chan Event
	done      chan struct{}
	closeOnce sync.Once
}

// Init puts the host console into raw mode and returns a Console.
func Init() (*Console, error) {
	in, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return nil, fmt.Errorf("console: stdin handle: %w", err)
	}
	out, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return nil, fmt.Errorf("console: stdout handle: %w", err)
	}

	var origIn, origOut uint32
	if err := windows.GetConsoleMode(in, &origIn); err != nil {
		return nil, fmt.Errorf("console: GetConsoleMode(in): %w", err)
	}
	if err := windows.GetConsoleMode(out, &origOut); err != nil {
		return nil, fmt.Errorf("console: GetConsoleMode(out): %w", err)
	}

	rawIn := origIn
	rawIn &^= enableProcessedInput | enableLineInput | enableEchoInput
	rawIn |= enableWindowInput | enableMouseInput | enableExtendedFlags
	if err := windows.SetConsoleMode(in, rawIn); err != nil {
		return nil, fmt.Errorf("console: SetConsoleMode(in): %w", err)
	}

	vtOut := origOut
	vtOut |= enableProcessedOutput | enableVirtualTermOutput | disableNewlineAutoRet
	if err := windows.SetConsoleMode(out, vtOut); err != nil {
		windows.SetConsoleMode(in, origIn)
		return nil, fmt.Errorf("console: SetConsoleMode(out): %w", err)
	}

	c := &Console{
		in:     in,
		out:    out,
		origIn: origIn,
		origOut: origOut,
		events: make(chan Event, 256),
		done:   make(chan struct{}),
	}
	go c.inputLoop()
	return c, nil
}

// Events returns the stream of console events.
func (c *Console) Events() <-chan Event {
	return c.events
}

// Size returns the current terminal size in cells.
func (c *Console) Size() (cols, rows int, err error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(c.out, &info); err != nil {
		return 0, 0, err
	}
	return int(info.Window.Right - info.Window.Left + 1), int(info.Window.Bottom - info.Window.Top + 1), nil
}

// WriteVT writes VT escape sequences to the host console.
func (c *Console) WriteVT(p []byte) (int, error) {
	var written uint32
	err := windows.WriteFile(c.out, p, &written, nil)
	if err != nil {
		return int(written), err
	}
	return int(written), nil
}

// Write implements io.Writer, forwarding bytes to the host console.
func (c *Console) Write(p []byte) (int, error) {
	return c.WriteVT(p)
}

// Close restores the original console modes and stops the input loop.
func (c *Console) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		windows.SetConsoleMode(c.out, c.origOut)
		windows.SetConsoleMode(c.in, c.origIn)
	})
}

// inputLoop reads console input records and translates them into events.
func (c *Console) inputLoop() {
	const maxRecords = 64
	var records [maxRecords]inputRecord
	for {
		select {
		case <-c.done:
			return
		default:
		}

		// The console input handle is waitable: it is signaled when input is
		// available. The timeout lets us observe cancellation.
		wait, err := windows.WaitForSingleObject(c.in, 100)
		if err != nil {
			return
		}
		if wait == uint32(windows.WAIT_TIMEOUT) {
			continue
		}

		var read uint32
		r1, _, err := procReadConsoleInputW.Call(
			uintptr(c.in),
			uintptr(unsafe.Pointer(&records[0])),
			uintptr(maxRecords),
			uintptr(unsafe.Pointer(&read)),
		)
		if r1 == 0 {
			return
		}

		for i := uint32(0); i < read; i++ {
			rec := &records[i]
			switch rec.eventType {
			case eventKey:
				ke := (*keyEventRecord)(unsafe.Pointer(&rec.union[0]))
				if ke.keyDown == 0 {
					continue
				}
				key := keyFromRecord(ke.virtualKeyCode, rune(ke.unicodeChar), ke.controlKeyState)
				if key.Code == 0 && key.Rune == 0 {
					continue
				}
				select {
				case c.events <- KeyEvent{Key: key}:
				case <-c.done:
					return
				}
			case eventMouse:
				me := (*mouseEventRecord)(unsafe.Pointer(&rec.union[0]))
				select {
				case c.events <- mouseFromRecord(me):
				case <-c.done:
					return
				}
			case eventWindowBufferSize:
				cols, rows, err := c.Size()
				if err != nil {
					continue
				}
				select {
				case c.events <- ResizeEvent{Cols: cols, Rows: rows}:
				case <-c.done:
					return
				}
			}
		}
	}
}