package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"weblinuxgui/input"

	"github.com/kbinani/screenshot"
	"github.com/pion/webrtc/v4"
)

//go:embed index.html
var embeddedFiles embed.FS

// No WebSocket client manager needed; WebRTC DataChannels are used instead.

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

func handleInput(ev InputEvent) {
	switch ev.Type {
	case "mousemove":
		input.MoveMouse(ev.X, ev.Y)
	case "mousedown":
		// no-op: some robotgo builds may not support press without click
	case "mouseup":
		input.Click(mapButton(ev.Button))
	case "contextmenu": // right-click
		input.Click(input.ButtonRight)
	case "wheel":
		// Implement scroll using input package. Positive deltaY scrolls down.
		input.Scroll(ev.DeltaY)
	case "keydown":
		if key := normalizeKey(ev.Key); key != "" {
			input.KeyDown(key)
		}
	case "keyup":
		if key := normalizeKey(ev.Key); key != "" {
			input.KeyUp(key)
		}
	case "paste":
		if ev.ClipboardText != "" {
			// Type the text directly
			input.TypeString(ev.ClipboardText)
		}
	}
}

func mapButton(b string) input.Button {
	switch strings.ToLower(b) {
	case "left", "l":
		return input.ButtonLeft
	case "right", "r":
		return input.ButtonRight
	case "center", "middle", "m":
		return input.ButtonMiddle
	default:
		return input.ButtonLeft
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
}

// captureAndEncode grabs the current display image and returns base64 JPEG and mouse coords.
func captureAndEncode(quality, display int) (b64 string, mx, my int, ok bool) {
	num := screenshot.NumActiveDisplays()
	if num <= 0 {
		return "", 0, 0, false
	}
	d := display
	if d < 0 || d >= num {
		d = 0
	}
	bounds := screenshot.GetDisplayBounds(d)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		// On some platforms capture can intermittently fail
		return "", 0, 0, false
	}

	// JPEG encode
	var buf bytes.Buffer
	q := quality
	if q <= 0 || q > 100 {
		q = 80
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		return "", 0, 0, false
	}
	b64 = base64.StdEncoding.EncodeToString(buf.Bytes())
	x, y := input.GetMousePos()
	return b64, x, y, true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	// Config
	addr := ":8080" // for serving index.html on Mac
	fps := 10
	quality := 80
	display := 0

	mode := strings.ToLower(os.Getenv("MODE")) // "server" on Mac, "peer" on Windows
	if mode == "" {
		// Default to server to avoid accidentally driving input on local machine
		mode = "server"
	}

	if os.Getenv("DISPLAY") == "" {
		// Keep previous behavior if unset (useful for X on Linux)
		os.Setenv("DISPLAY", ":0")
	}

	switch mode {
	case "server":
		runServer(addr)
	case "peer":
		if err := runPeer(fps, quality, display); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown MODE=%s (use 'server' or 'peer')", mode)
	}
}

func runServer(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: addr, Handler: mux}

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
	log.Println("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Println("server shutdown error:", err)
	}
}

func runPeer(fps, quality, display int) error {
	// Create PeerConnection with Google STUN
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})
	if err != nil {
		return fmt.Errorf("new pc: %w", err)
	}
	defer pc.Close()

	// Channels
	var framesDC *webrtc.DataChannel
	inputReady := make(chan struct{})
	framesReady := make(chan struct{})

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		label := dc.Label()
		log.Println("data channel:", label)
		switch label {
		case "input":
			dc.OnOpen(func() {
				close(inputReady)
			})
			dc.OnMessage(func(msg webrtc.DataChannelMessage) {
				if msg.IsString {
					var ev InputEvent
					if err := json.Unmarshal(msg.Data, &ev); err == nil {
						handleInput(ev)
					}
				}
			})
		case "frames":
			framesDC = dc
			dc.OnOpen(func() {
				close(framesReady)
			})
		default:
			// ignore
		}
	})

	// Read remote offer (base64 JSON of SessionDescription)
	fmt.Println("Paste base64 Offer (from browser) then press Enter, then Ctrl-D (EOF) if multi-line:")
	offerB64, err := readAllStdin()
	if err != nil {
		return err
	}
	offerJSON, err := base64.StdEncoding.DecodeString(strings.TrimSpace(offerB64))
	if err != nil {
		return fmt.Errorf("decode offer b64: %w", err)
	}
	var offer webrtc.SessionDescription
	if err := json.Unmarshal(offerJSON, &offer); err != nil {
		return fmt.Errorf("unmarshal offer: %w", err)
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		return fmt.Errorf("set remote: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("create answer: %w", err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("set local: %w", err)
	}
	<-gatherComplete
	local := pc.LocalDescription()
	ansJSON, _ := json.Marshal(local)
	ansB64 := base64.StdEncoding.EncodeToString(ansJSON)
	fmt.Println("Answer (base64). Copy back into browser:\n" + ansB64)

	// Wait for frames channel ready
	select {
	case <-framesReady:
		log.Println("frames channel ready; starting stream")
	case <-time.After(30 * time.Second):
		return fmt.Errorf("frames channel not opened by browser")
	}

	// Start streaming loop
	stop := make(chan struct{})
	go func() {
		interval := time.Second / time.Duration(max(fps, 1))
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				b64, mx, my, ok := captureAndEncode(quality, display)
				if !ok || framesDC == nil {
					continue
				}
				payload, _ := json.Marshal(struct {
					Image  string `json:"image"`
					MouseX int    `json:"mouseX"`
					MouseY int    `json:"mouseY"`
				}{Image: b64, MouseX: mx, MouseY: my})
				_ = framesDC.SendText(string(payload))
			}
		}
	}()

	// Keep running until interrupted
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	close(stop)
	return nil
}

func readAllStdin() (string, error) {
	// Read a single line (most base64 offers will be single-line). If more data
	// is piped in, read the rest.
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	// Drain remaining bytes if any
	rest, _ := io.ReadAll(reader)
	return line + string(rest), nil
}
