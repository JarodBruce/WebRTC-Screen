package input

import (
	"log"

	t "weblinuxgui/internal/types"

	"github.com/go-vgo/robotgo"
)

// HandleEvent executes the side-effect for a control Event
func HandleEvent(event t.Event) {
	switch event.Type {
	case "mousemove":
		robotgo.Move(event.X, event.Y)
	case "mousedown":
		robotgo.Toggle(event.Button, "down")
	case "mouseup":
		robotgo.Toggle(event.Button, "up")
	case "keydown":
		robotgo.KeyTap(event.Key, event.Modifiers)
	case "keyup":
		// no-op, could use robotgo.KeyToggle if needed
	case "wheel":
		robotgo.Scroll(0, event.DeltaY)
	case "paste":
		if err := robotgo.WriteAll(event.ClipboardText); err != nil {
			log.Println("clipboard write error:", err)
		}
	}
}
