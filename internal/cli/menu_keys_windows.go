//go:build windows

package cli

import (
	"encoding/binary"
	"io"
	"syscall"
	"unsafe"
)

const (
	consoleKeyEvent = 0x0001
	enableLineInput = 0x0002
	enableEchoInput = 0x0004
	enableVTOutput  = 0x0004
)

var (
	consoleKernel            = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode       = consoleKernel.NewProc("GetConsoleMode")
	procSetConsoleMode       = consoleKernel.NewProc("SetConsoleMode")
	procReadConsoleInputW    = consoleKernel.NewProc("ReadConsoleInputW")
)

type windowsHomeKeyboard struct {
	input, output syscall.Handle
	inputMode, outputMode uint32
}

// The Windows console supplies actual virtual-key events for PowerShell/CMD:
// arrows do not depend on whether a shell emits ANSI input sequences.
// We only enable VT *output* long enough to redraw the highlight.
func openHomeKeyboard() (homeKeyboard, bool) {
	input, err := syscall.GetStdHandle(syscall.STD_INPUT_HANDLE)
	if err != nil {
		return nil, false
	}
	output, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil {
		return nil, false
	}
	var inMode, outMode uint32
	if !getWindowsConsoleMode(input, &inMode) || !getWindowsConsoleMode(output, &outMode) {
		return nil, false
	}
	if !setWindowsConsoleMode(output, outMode|enableVTOutput) {
		return nil, false
	}
	if !setWindowsConsoleMode(input, inMode &^ (enableLineInput | enableEchoInput)) {
		setWindowsConsoleMode(output, outMode)
		return nil, false
	}
	return &windowsHomeKeyboard{input:input,output:output,inputMode:inMode,outputMode:outMode}, true
}

func getWindowsConsoleMode(handle syscall.Handle, mode *uint32) bool {
	result, _, _ := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(mode)))
	return result != 0
}

func setWindowsConsoleMode(handle syscall.Handle, mode uint32) bool {
	result, _, _ := procSetConsoleMode.Call(uintptr(handle), uintptr(mode))
	return result != 0
}

func (k *windowsHomeKeyboard) close() {
	setWindowsConsoleMode(k.input, k.inputMode)
	setWindowsConsoleMode(k.output, k.outputMode)
}

func (k *windowsHomeKeyboard) readKey() (homeKey, error) {
	// INPUT_RECORD: EventType (2), padding (2), KEY_EVENT_RECORD (16).
	var record [20]byte
	for {
		var eventsRead uint32
		n, _, err := procReadConsoleInputW.Call(
			uintptr(k.input),
			uintptr(unsafe.Pointer(&record[0])),
			1,
			uintptr(unsafe.Pointer(&eventsRead)),
		)
		if n == 0 {
			if err != nil && err != syscall.Errno(0) {
				return "", err
			}
			return "", io.EOF
		}
		if eventsRead == 0 || binary.LittleEndian.Uint16(record[0:2]) != consoleKeyEvent {
			continue
		}
		if binary.LittleEndian.Uint32(record[4:8]) == 0 { // key-up event
			continue
		}
		virtualKey := binary.LittleEndian.Uint16(record[10:12])
		char := binary.LittleEndian.Uint16(record[14:16])
		switch virtualKey {
		case 0x26:
			return homeKeyUp, nil
		case 0x28:
			return homeKeyDown, nil
		case 0x0D:
			return homeKeyEnter, nil
		case 0x1B:
			return homeKeyEscape, nil
		}
		if char >= '0' && char <= '4' {
			return homeKey(string(rune(char))), nil
		}
	}
}
