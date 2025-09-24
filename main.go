package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"image/jpeg"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-vgo/robotgo"
	"github.com/gorilla/websocket"
	"github.com/kbinani/screenshot"
)

//go:embed index.html
var embeddedFiles embed.FS

// ---- Client manager ----
type ClientManager struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
}

func NewManager() *ClientManager {
	return &ClientManager{clients: make(map[*websocket.Conn]bool)}
}

func (m *ClientManager) Add(c *websocket.Conn) {
	m.mu.Lock()
	m.clients[c] = true
	m.mu.Unlock()
}

func (m *ClientManager) Remove(c *websocket.Conn) {
	m.mu.Lock()
	if _, ok := m.clients[c]; ok {
		delete(m.clients, c)
		_ = c.Close()
	}
	m.mu.Unlock()
}

func (m *ClientManager) BroadcastJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Println("broadcast marshal error:", err)
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for c := range m.clients {
		c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Println("write client error:", err)
		}
	}
}

// ---- HTTP handlers ----
func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	b, err := embeddedFiles.ReadFile("index.html")
	if err != nil {
		http.Error(w, "index missing", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(b)
}

// ---- WebSocket and input handling ----
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type InputEvent struct {
	Type          string   `json:"type"`
	Key           string   `json:"key"`
	KeyCode       int      `json:"keyCode"`
	Modifiers     []string `json:"modifiers"`
	DeltaY        float64  `json:"deltaY"`
	X             int      `json:"x"`
	Y             int      `json:"y"`
	Button        string   `json:"button"`
	ClipboardText string   `json:"clipboardText"`
}

func handleWS(mgr *ClientManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("upgrade error:", err)
			return
		}
		mgr.Add(conn)
		log.Println("client connected")

		defer func() {
			mgr.Remove(conn)
			log.Println("client disconnected")
		}()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var ev InputEvent
			if err := json.Unmarshal(msg, &ev); err != nil {
				log.Println("ws json error:", err)
				continue
			}
			handleInput(ev)
		}
	}
}

func handleInput(ev InputEvent) {
	switch ev.Type {
	case "mousemove":
		robotgo.MoveMouse(ev.X, ev.Y)
	case "mousedown":
		// no-op: some robotgo builds may not support press without click
	case "mouseup":
		robotgo.Click(normalizeButton(ev.Button), false)
	case "contextmenu": // right-click
		robotgo.Click("right", false)
	case "wheel":
		// Scrolling not supported in this build to keep compatibility across OS variants
		// Consider mapping to key events (PageUp/PageDown) if needed
	case "keydown":
		if key := normalizeKey(ev.Key); key != "" {
			robotgo.KeyDown(key)
		}
	case "keyup":
		if key := normalizeKey(ev.Key); key != "" {
			robotgo.KeyUp(key)
		}
	case "paste":
		if ev.ClipboardText != "" {
			// Type the text directly
			robotgo.TypeStr(ev.ClipboardText)
		}
	}
}

func normalizeButton(b string) string {
	switch strings.ToLower(b) {
	case "left", "l":
		return "left"
	case "right", "r":
		return "right"
	case "center", "middle", "m":
		return "center"
	default:
		return "left"
	}
}

func normalizeKey(k string) string {
	k = strings.ToLower(k)
	switch k {
	case "enter":
		return "enter"
	case "shift":
		return "shift"
	case "control", "ctrl":
		return "ctrl"
	case "alt", "option":
		return "alt"
	case "meta", "command", "cmd":
		return "cmd" // on macOS
	case "escape", "esc":
		return "esc"
	case " ", "space":
		return "space"
	case "tab":
		return "tab"
	case "backspace":
		return "backspace"
	case "delete":
		return "delete"
	case "arrowup":
		return "up"
	case "arrowdown":
		return "down"
	case "arrowleft":
		return "left"
	case "arrowright":
		return "right"
	default:
		// If it's a single character, assume it's a plain key (letters, digits, symbols)
		if len(k) == 1 {
			return k
		}
		return ""
	}
}

// ---- Streaming ----
type StreamConfig struct {
	FPS     int
	Quality int
	Display int
	Manager *ClientManager
}

func StartStreamer(cfg StreamConfig) chan struct{} {
	stop := make(chan struct{})
	go func() {
		interval := time.Second / time.Duration(max(cfg.FPS, 1))
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				sendFrame(cfg)
			}
		}
	}()
	return stop
}

func sendFrame(cfg StreamConfig) {
	num := screenshot.NumActiveDisplays()
	if num <= 0 {
		return
	}
	d := cfg.Display
	if d < 0 || d >= num {
		d = 0
	}
	bounds := screenshot.GetDisplayBounds(d)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		// On some platforms capture can intermittently fail
		return
	}

	// JPEG encode
	var buf bytes.Buffer
	q := cfg.Quality
	if q <= 0 || q > 100 {
		q = 80
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		return
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	x, y := robotgo.GetMousePos()

	cfg.Manager.BroadcastJSON(struct {
		Image  string `json:"image"`
		MouseX int    `json:"mouseX"`
		MouseY int    `json:"mouseY"`
	}{Image: b64, MouseX: x, MouseY: y})
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	// Fixed configuration (no flags/args as requested)
	addr := ":8080"
	fps := 10
	quality := 80
	display := 0

	if os.Getenv("DISPLAY") == "" {
		// Keep previous behavior if unset (useful for X on Linux)
		os.Setenv("DISPLAY", ":0")
	}

	mgr := NewManager()

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/ws", handleWS(mgr))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: addr, Handler: mux}

	// Start streamer loop
	stopStream := StartStreamer(StreamConfig{FPS: fps, Quality: quality, Display: display, Manager: mgr})

	// Run HTTP server
	go func() {
		log.Println("http server started on", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("ListenAndServe:", err)
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	log.Println("shutting down...")

	close(stopStream)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Println("server shutdown error:", err)
	}
}
