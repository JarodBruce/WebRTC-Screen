//go:build windows

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"weblinuxgui/input"

	"github.com/kbinani/screenshot"
	"github.com/pion/webrtc/v4"
)

// InputEvent mirrors the browser-sent event structure
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

// Global display offset for multi-monitor setups
var dispOffsetX, dispOffsetY int

func handleInput(ev InputEvent) {
	switch ev.Type {
	case "mousemove":
		// Adjust for display origin in multi-monitor setups
		input.MoveMouse(ev.X+dispOffsetX, ev.Y+dispOffsetY)
	case "mousedown":
		// Ensure cursor is at target before any press logic
		input.MoveMouse(ev.X+dispOffsetX, ev.Y+dispOffsetY)
	case "mouseup":
		// Ensure cursor is at target even if no prior mousemove arrived
		input.MoveMouse(ev.X+dispOffsetX, ev.Y+dispOffsetY)
		input.Click(mapButton(ev.Button))
	case "contextmenu":
		// Right click at target location
		input.MoveMouse(ev.X+dispOffsetX, ev.Y+dispOffsetY)
		input.Click(input.ButtonRight)
	case "wheel":
		// Scroll at target location
		input.MoveMouse(ev.X+dispOffsetX, ev.Y+dispOffsetY)
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
		return "cmd"
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
		if len(k) == 1 {
			return k
		}
		return ""
	}
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
	dispOffsetX, dispOffsetY = bounds.Min.X, bounds.Min.Y
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return "", 0, 0, false
	}

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

func runPeer(fps, quality, display int) error {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})
	if err != nil {
		return fmt.Errorf("new pc: %w", err)
	}
	defer pc.Close()

	var framesDC *webrtc.DataChannel
	framesReady := make(chan struct{})

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		label := dc.Label()
		log.Println("data channel:", label)
		switch label {
		case "input":
			dc.OnOpen(func() { log.Println("input data channel open") })
			dc.OnMessage(func(msg webrtc.DataChannelMessage) {
				if msg.IsString {
					var ev InputEvent
					if err := json.Unmarshal(msg.Data, &ev); err == nil {
						// Process input event without extra logging
						handleInput(ev)
					}
				}
			})
		case "frames":
			framesDC = dc
			dc.OnOpen(func() { close(framesReady) })
		}
	})

	// UDP signaling: listen for OFFER and reply with ANSWER
	getEnv := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	getEnvBool := func(k string, def bool) bool {
		if v := os.Getenv(k); v != "" {
			v = strings.ToLower(strings.TrimSpace(v))
			return v == "1" || v == "true" || v == "yes"
		}
		return def
	}
	udpPort := getEnv("UDP_PORT", "8080")
	localhostOnly := getEnvBool("BIND_LOCALHOST_ONLY", false)
	// LOCAL_ADDR can be either "ip:port" or just "ip"; if empty, fall back to 0.0.0.0:UDP_PORT (or 127.0.0.1 when localhost only)
	bindAddr := getEnv("LOCAL_ADDR", "")
	if bindAddr == "" {
		if localhostOnly {
			bindAddr = net.JoinHostPort("127.0.0.1", udpPort)
		} else {
			bindAddr = net.JoinHostPort("0.0.0.0", udpPort)
		}
	} else if !strings.Contains(bindAddr, ":") {
		// If user provided only an IP, append UDP_PORT automatically
		bindAddr = net.JoinHostPort(bindAddr, udpPort)
	}
	localAddr, err := net.ResolveUDPAddr("udp4", bindAddr)
	if err != nil {
		return fmt.Errorf("resolve local UDP: %w", err)
	}
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		return fmt.Errorf("listen UDP: %w", err)
	}
	defer conn.Close()
	log.Println("Windows peer UDP listening on", bindAddr)

	offerStr, from, err := waitForPrefix(conn, "OFFER:", 60*time.Second)
	if err != nil {
		return fmt.Errorf("wait OFFER: %w", err)
	}
	offerJSON, err := base64.StdEncoding.DecodeString(strings.TrimSpace(offerStr))
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
	// Send ANSWER back to the sender via UDP
	if _, err := conn.WriteToUDP([]byte("ANSWER:"+ansB64), from); err != nil {
		return fmt.Errorf("send ANSWER: %w", err)
	}
	log.Println("ANSWER sent via UDP to", from.String())
	log.Println("Waiting for data channels to open...")

	select {
	case <-framesReady:
		log.Println("frames channel ready; starting stream")
	case <-time.After(30 * time.Second):
		return fmt.Errorf("frames channel not opened by browser")
	}

	// Chunked transfer to respect SCTP/DC message size limits
	// Keep chunks small (<16KB) to be safe across browsers/OSes
	const chunkSize = 12 * 1024
	interval := time.Second / time.Duration(max(fps, 1))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	frameID := 0
	for range ticker.C {
		if framesDC == nil {
			continue
		}
		b64, mx, my, ok := captureAndEncode(quality, display)
		if !ok || len(b64) == 0 {
			continue
		}
		nChunks := (len(b64) + chunkSize - 1) / chunkSize
		// Send metadata first
		meta := struct {
			Type   string `json:"type"`
			ID     int    `json:"id"`
			Chunks int    `json:"chunks"`
			MouseX int    `json:"mouseX"`
			MouseY int    `json:"mouseY"`
		}{Type: "frameMeta", ID: frameID, Chunks: nChunks, MouseX: mx, MouseY: my}
		if err := framesDC.SendText(mustJSON(meta)); err != nil {
			// If we fail to send meta, skip this frame
			continue
		}
		// Send chunks
		for i := 0; i < nChunks; i++ {
			start := i * chunkSize
			end := start + chunkSize
			if end > len(b64) {
				end = len(b64)
			}
			chunk := struct {
				Type  string `json:"type"`
				ID    int    `json:"id"`
				Index int    `json:"index"`
				Data  string `json:"data"`
			}{Type: "frameChunk", ID: frameID, Index: i, Data: b64[start:end]}
			// Best-effort; drop frame if a chunk fails, next frame will arrive soon
			_ = framesDC.SendText(mustJSON(chunk))
		}
		frameID++
		if frameID == int(^uint(0)>>1) { // avoid overflow; reset occasionally
			frameID = 0
		}
	}
	return nil
}

// waitForPrefix reads UDP packets until one starting with the given prefix arrives, or timeout.
func waitForPrefix(conn *net.UDPConn, prefix string, timeout time.Duration) (string, *net.UDPAddr, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 64*1024)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return "", nil, err
		}
		msg := string(buf[:n])
		if strings.HasPrefix(msg, prefix) {
			return msg[len(prefix):], addr, nil
		}
		// ignore other packets
	}
}

// envInt reads an environment variable or returns a default if unset/invalid.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Windows entry point so this file can be run directly: `go run ./windows.go`
func main() {
	fps := envInt("FPS", 10)
	quality := envInt("QUALITY", 80)
	display := envInt("DISPLAY_INDEX", 0)
	if err := runPeer(fps, quality, display); err != nil {
		log.Fatal(err)
	}
}

// mustJSON is a tiny helper to marshal or return an empty JSON object on error.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
