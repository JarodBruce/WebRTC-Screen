//go:build windows

package input

import (
	"syscall"
	"unicode"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procGetCursorPos = user32.NewProc("GetCursorPos")
	procMouseEvent   = user32.NewProc("mouse_event")
	procKeybdEvent   = user32.NewProc("keybd_event")
)

// Win32 constants
const (
	// mouse buttons
	MOUSEEVENTF_MOVE       = 0x0001
	MOUSEEVENTF_LEFTDOWN   = 0x0002
	MOUSEEVENTF_LEFTUP     = 0x0004
	MOUSEEVENTF_RIGHTDOWN  = 0x0008
	MOUSEEVENTF_RIGHTUP    = 0x0010
	MOUSEEVENTF_MIDDLEDOWN = 0x0020
	MOUSEEVENTF_MIDDLEUP   = 0x0040

	// key flags
	KEYEVENTF_KEYUP = 0x0002

	// virtual keys
	VK_BACK    = 0x08
	VK_TAB     = 0x09
	VK_RETURN  = 0x0D
	VK_SHIFT   = 0x10
	VK_CONTROL = 0x11
	VK_MENU    = 0x12 // ALT
	VK_ESCAPE  = 0x1B
	VK_SPACE   = 0x20
	VK_LEFT    = 0x25
	VK_UP      = 0x26
	VK_RIGHT   = 0x27
	VK_DOWN    = 0x28
	VK_DELETE  = 0x2E
	VK_LWIN    = 0x5B

	VK_OEM_1      = 0xBA // ;:
	VK_OEM_PLUS   = 0xBB // =+
	VK_OEM_COMMA  = 0xBC // ,<
	VK_OEM_MINUS  = 0xBD // -_
	VK_OEM_PERIOD = 0xBE // .>
	VK_OEM_2      = 0xBF // /?
	VK_OEM_3      = 0xC0 // `~
	VK_OEM_4      = 0xDB // [{
	VK_OEM_5      = 0xDC // \|
	VK_OEM_6      = 0xDD // ]}
	VK_OEM_7      = 0xDE // '"
)

type point struct {
	X int32
	Y int32
}

// Internal platform functions
func moveMouse(x, y int) {
	procSetCursorPos.Call(uintptr(int32(x)), uintptr(int32(y)))
}

func getMousePos() (int, int) {
	var p point
	ret, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	if ret == 0 {
		return 0, 0
	}
	return int(p.X), int(p.Y)
}

func click(btn Button) {
	var down, up uintptr
	switch btn {
	case ButtonLeft:
		down, up = MOUSEEVENTF_LEFTDOWN, MOUSEEVENTF_LEFTUP
	case ButtonRight:
		down, up = MOUSEEVENTF_RIGHTDOWN, MOUSEEVENTF_RIGHTUP
	case ButtonMiddle:
		down, up = MOUSEEVENTF_MIDDLEDOWN, MOUSEEVENTF_MIDDLEUP
	default:
		down, up = MOUSEEVENTF_LEFTDOWN, MOUSEEVENTF_LEFTUP
	}
	mouseEvent(uint32(down), 0, 0, 0, 0)
	mouseEvent(uint32(up), 0, 0, 0, 0)
}

func mouseEvent(flags uint32, dx, dy int32, data uint32, extra uintptr) {
	procMouseEvent.Call(
		uintptr(flags),
		uintptr(dx),
		uintptr(dy),
		uintptr(data),
		extra,
	)
}

func keyDown(name string) {
	if vk, shift := mapKey(name); vk != 0 {
		if shift {
			keybdEvent(VK_SHIFT, 0, 0, 0)
		}
		keybdEvent(vk, 0, 0, 0)
	}
}

func keyUp(name string) {
	if vk, shift := mapKey(name); vk != 0 {
		keybdEvent(vk, 0, KEYEVENTF_KEYUP, 0)
		if shift {
			keybdEvent(VK_SHIFT, 0, KEYEVENTF_KEYUP, 0)
		}
	}
}

func keybdEvent(vk uint16, scan uint8, flags uint32, extra uintptr) {
	procKeybdEvent.Call(
		uintptr(vk),
		uintptr(scan),
		uintptr(flags),
		extra,
	)
}

func typeString(s string) {
	for _, r := range s {
		if vk, shift := mapRune(r); vk != 0 {
			if shift {
				keybdEvent(VK_SHIFT, 0, 0, 0)
			}
			keybdEvent(vk, 0, 0, 0)
			keybdEvent(vk, 0, KEYEVENTF_KEYUP, 0)
			if shift {
				keybdEvent(VK_SHIFT, 0, KEYEVENTF_KEYUP, 0)
			}
		}
	}
}

// mapKey maps normalized key names to virtual-key codes.
func mapKey(name string) (vk uint16, needsShift bool) {
	switch name {
	case "enter":
		return VK_RETURN, false
	case "shift":
		return VK_SHIFT, false
	case "ctrl":
		return VK_CONTROL, false
	case "alt":
		return VK_MENU, false
	case "cmd", "win", "meta":
		return VK_LWIN, false
	case "esc":
		return VK_ESCAPE, false
	case "space":
		return VK_SPACE, false
	case "tab":
		return VK_TAB, false
	case "backspace":
		return VK_BACK, false
	case "delete":
		return VK_DELETE, false
	case "up":
		return VK_UP, false
	case "down":
		return VK_DOWN, false
	case "left":
		return VK_LEFT, false
	case "right":
		return VK_RIGHT, false
	}
	// single character keys
	if len(name) == 1 {
		r := rune(name[0])
		return mapRune(r)
	}
	return 0, false
}

func mapRune(r rune) (vk uint16, needsShift bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return uint16('A' + (r - 'a')), false
	case r >= 'A' && r <= 'Z':
		return uint16(r), true
	case r >= '0' && r <= '9':
		return uint16(r), false
	}
	switch r {
	case ' ':
		return VK_SPACE, false
	case '\n':
		return VK_RETURN, false
	case '.':
		return VK_OEM_PERIOD, false
	case ',':
		return VK_OEM_COMMA, false
	case '-':
		return VK_OEM_MINUS, false
	case '_':
		return VK_OEM_MINUS, true
	case '=':
		return VK_OEM_PLUS, false
	case '+':
		return VK_OEM_PLUS, true
	case ';':
		return VK_OEM_1, false
	case ':':
		return VK_OEM_1, true
	case '/':
		return VK_OEM_2, false
	case '?':
		return VK_OEM_2, true
	case '`':
		return VK_OEM_3, false
	case '~':
		return VK_OEM_3, true
	case '[':
		return VK_OEM_4, false
	case '{':
		return VK_OEM_4, true
	case '\\':
		return VK_OEM_5, false
	case '|':
		return VK_OEM_5, true
	case ']':
		return VK_OEM_6, false
	case '}':
		return VK_OEM_6, true
	case '\'':
		return VK_OEM_7, false
	case '"':
		return VK_OEM_7, true
	}
	// best-effort
	if unicode.IsLetter(r) {
		rr := unicode.ToUpper(r)
		if rr <= 0xFFFF {
			return uint16(rr), false
		}
	}
	return 0, false
}
