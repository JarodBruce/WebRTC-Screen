package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/jpeg"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-vgo/robotgo"
	"github.com/gorilla/websocket"
	"github.com/kbinani/screenshot"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Event struct for incoming messages from the client
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

// ScreenUpdate struct for outgoing messages to the client
type ScreenUpdate struct {
	Image  string `json:"image"`
	MouseX int    `json:"mouseX"`
	MouseY int    `json:"mouseY"`
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	log.Println("Serving index.html")
	http.ServeFile(w, r, "index.html")
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()
	log.Println("Client Connected")

	go handleInput(ws)
	streamScreen(ws)
}

func handleInput(ws *websocket.Conn) {
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Println("read error:", err)
			break
		}

		var event Event
		if err := json.Unmarshal(msg, &event); err != nil {
			log.Println("error unmarshalling json:", err)
			continue
		}

		switch event.Type {
		case "mousemove":
			robotgo.Move(event.X, event.Y)
		case "mousedown":
			robotgo.Toggle(event.Button, "down")
		case "mouseup":
			robotgo.Toggle(event.Button, "up")
		case "keydown":
			// robotgo doesn't handle all keys well (e.g. symbols).
			// A more robust solution might be needed for special characters.
			robotgo.KeyTap(event.Key, event.Modifiers)
		case "keyup":
			// Keyup is often not needed for KeyTap, but could be implemented
			// with KeyToggle if necessary for specific applications (e.g. games).
		case "wheel":
			robotgo.Scroll(0, event.DeltaY)
		case "paste":
			robotgo.WriteAll(event.ClipboardText)
		}
	}
}

func streamScreen(ws *websocket.Conn) {
	ticker := time.NewTicker(100 * time.Millisecond) // 10 FPS
	defer ticker.Stop()

	for range ticker.C {
		bounds := screenshot.GetDisplayBounds(0)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			log.Println("capture error:", err)
			continue
		}

		buf := new(bytes.Buffer)
		err = jpeg.Encode(buf, img, &jpeg.Options{Quality: 80})
		if err != nil {
			log.Println("jpeg encode error:", err)
			continue
		}

		imgStr := base64.StdEncoding.EncodeToString(buf.Bytes())
		mouseX, mouseY := robotgo.GetMousePos()

		update := ScreenUpdate{
			Image:  imgStr,
			MouseX: mouseX,
			MouseY: mouseY,
		}

		jsonUpdate, err := json.Marshal(update)
		if err != nil {
			log.Println("json marshal error:", err)
			continue
		}

		err = ws.WriteMessage(websocket.TextMessage, jsonUpdate)
		if err != nil {
			log.Println("write error:", err)
			break
		}
	}
}

func main() {
	os.Setenv("DISPLAY", ":0")

	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", handleConnections)

	log.Println("http server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
