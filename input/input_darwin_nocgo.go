//go:build darwin && !cgo

package input

// Pure-Go no-op shims when building on macOS without cgo.

func moveMouse(x, y int)      {}
func getMousePos() (int, int) { return 0, 0 }
func click(btn Button)        {}
func keyDown(name string)     {}
func keyUp(name string)       {}
func typeString(s string)     {}
func scroll(deltaY float64)   {}
