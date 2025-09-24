package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"weblinuxgui/internal/capture"
	"weblinuxgui/internal/clients"
	in "weblinuxgui/internal/input"
	t "weblinuxgui/internal/types"

	"github.com/gorilla/websocket"
)

type Config struct {
	FPS     int
	Quality int
	Display int
	Manager *clients.Manager
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func ServeIndex(w http.ResponseWriter, r *http.Request) {
	log.Println("Serving index.html")
	http.ServeFile(w, r, "index.html")
}

// HandleWS handles websocket connections for control and stream roles.
func HandleWS(mgr *clients.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := r.URL.Query().Get("role")
		clientID := r.URL.Query().Get("clientId")
		if clientID == "" {
			clientID = "default"
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("upgrade error:", err)
			return
		}

		ws.SetReadLimit(8 << 20) // 8MB
		ws.SetReadDeadline(time.Now().Add(60 * time.Second))
		ws.SetPongHandler(func(appData string) error {
			ws.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})

		log.Printf("New websocket connection role=%s client=%s\n", role, clientID)

		switch role {
		case "stream":
			mgr.AddStream(clientID, ws)
			go func(conn *websocket.Conn) {
				defer func() {
					mgr.RemoveStream(clientID, conn)
					conn.Close()
				}()
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						log.Println("stream read close:", err)
						return
					}
				}
			}(ws)
		default: // control
			old := mgr.SetControl(clientID, ws)
			if old != nil {
				old.Close()
			}
			go handleInput(clientID, mgr, ws)
		}
	}
}

func handleInput(clientID string, mgr *clients.Manager, ws *websocket.Conn) {
	defer func() {
		mgr.RemoveControl(clientID, ws)
		ws.Close()
	}()

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Println("control read error:", err)
			return
		}
		var ev t.Event
		if err := json.Unmarshal(msg, &ev); err != nil {
			log.Println("json error:", err)
			continue
		}
		in.HandleEvent(ev)
	}
}

// StartStreamer launches a ticker-based loop that captures frames and dispatches
// them to each client's chosen target (round-robin across stream conns with
// fallback to control).
func StartStreamer(cfg Config) chan struct{} {
	stop := make(chan struct{})
	fps := cfg.FPS
	if fps <= 0 {
		fps = 10
	}
	interval := time.Second / time.Duration(fps)
	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				img, mx, my, err := capture.Frame(capture.Options{Display: cfg.Display, Quality: cfg.Quality})
				if err != nil {
					log.Println("capture error:", err)
					continue
				}
				payload, _ := json.Marshal(t.ScreenUpdate{Image: img, MouseX: mx, MouseY: my})

				// Send to each client separately.
				cfg.Manager.ForEachClient(func(id string) {
					conn := cfg.Manager.NextTarget(id)
					if conn == nil {
						return
					}
					conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
						log.Println("write error:", err)
					}
				})

			case <-stop:
				return
			}
		}
	}()
	return stop
}
