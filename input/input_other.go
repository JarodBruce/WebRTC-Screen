//go:build !windows

package input

// Minimal no-op implementations for non-Windows platforms so the project
// remains buildable on macOS/Linux when developing.

func moveMouse(x, y int) {}

func getMousePos() (int, int) { return 0, 0 }

func click(btn Button) {}

func keyDown(name string) {}

func keyUp(name string) {}

func typeString(s string) {}
