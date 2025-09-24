package input

// Package input provides a tiny cross-platform abstraction over a few
// keyboard/mouse operations we need. Each platform implements these in
// separate files guarded by build tags.

type Button string

const (
	ButtonLeft   Button = "left"
	ButtonRight  Button = "right"
	ButtonMiddle Button = "middle"
)

// MoveMouse moves the cursor to absolute screen coordinates (x,y).
func MoveMouse(x, y int) { moveMouse(x, y) }

// Click performs a mouse click with the given button.
func Click(btn Button) { click(btn) }

// GetMousePos returns the current cursor position.
func GetMousePos() (x, y int) { return getMousePos() }

// KeyDown presses a virtual key by name (best-effort mapping).
func KeyDown(k string) { keyDown(k) }

// KeyUp releases a virtual key by name.
func KeyUp(k string) { keyUp(k) }

// TypeString types text using synthetic keyboard events.
func TypeString(s string) { typeString(s) }

// Scroll vertically by a delta measured in browser-style pixels.
// Positive delta scrolls down, negative scrolls up.
func Scroll(deltaY float64) { scroll(deltaY) }
