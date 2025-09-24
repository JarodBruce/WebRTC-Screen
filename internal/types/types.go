package types

// Event represents incoming control messages from the client
type Event struct {
	Type          string   `json:"type"`
	X             int      `json:"x"`
	Y             int      `json:"y"`
	Button        string   `json:"button"`
	Key           string   `json:"key"`
	KeyCode       int      `json:"keyCode"`
	Modifiers     []string `json:"modifiers"`
	DeltaY        int      `json:"deltaY"`
	ClipboardText string   `json:"clipboardText"`
}

// ScreenUpdate is the outbound frame + cursor payload
type ScreenUpdate struct {
	Image  string `json:"image"`
	MouseX int    `json:"mouseX"`
	MouseY int    `json:"mouseY"`
}
