package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/go-vgo/robotgo"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{} // use default options

type MouseEvent struct {
	Type   string `json:"type"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Button int    `json:"button"`
	Key    string `json:"key"`
	KeyCode int   `json:"keyCode"`
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	log.Println("Serving index.html")
	http.ServeFile(w, r, "index.html")
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Upgrade initial GET request to a websocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	// Make sure we close the connection when the function returns
	defer ws.Close()

	log.Println("Client Connected")

	for {
		// Read message from browser
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}

		var event MouseEvent
		if err := json.Unmarshal(msg, &event); err != nil {
			log.Println("error unmarshalling json:", err)
			continue
		}

		switch event.Type {
		case "mousemove":
			robotgo.Move(event.X, event.Y)
		case "mousedown":
			robotgo.Click("left", false) // false means press down, not release
		case "mouseup":
			robotgo.Click("left", true) // true means release
		case "keydown":
			robotgo.KeyTap(event.Key)
		case "keyup":
			// Key release is not explicitly handled by robotgo in a simple way
			// for KeyTap. KeyTap simulates a full press and release.
		}
	}
}

func main() {
	// This is often required for robotgo to find the display.
	os.Setenv("DISPLAY", ":0")

	// Configure http server
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", handleConnections)

	// Start server
	log.Println("http server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
