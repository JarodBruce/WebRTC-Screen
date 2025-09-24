//go:build windows

package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"log"
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

func handleInput(ev InputEvent) {
	switch ev.Type {
	case "mousemove":
		input.MoveMouse(ev.X, ev.Y)
	case "mousedown":
		// optional: hold press logic
	case "mouseup":
		input.Click(mapButton(ev.Button))
	case "contextmenu":
		input.Click(input.ButtonRight)
	case "wheel":
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
			dc.OnOpen(func() { close(framesReady) })
		}
	})

	offerB64, err := readBase64FromPrompt("Paste Offer (base64) from browser. End with an empty line or type END on a new line:\n> ")
	if err != nil {
		return fmt.Errorf("read offer: %w", err)
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
	fmt.Println("\nAnswer (base64) — copy this back into the browser:")
	fmt.Println(ansB64)
	fmt.Println("\nWaiting for data channels to open...")

	select {
	case <-framesReady:
		log.Println("frames channel ready; starting stream")
	case <-time.After(30 * time.Second):
		return fmt.Errorf("frames channel not opened by browser")
	}

	interval := time.Second / time.Duration(max(fps, 1))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if framesDC == nil {
			continue
		}
		b64, mx, my, ok := captureAndEncode(quality, display)
		if !ok {
			continue
		}
		payload, _ := json.Marshal(struct {
			Image          string `json:"image"`
			MouseX, MouseY int
		}{Image: b64, MouseX: mx, MouseY: my})
		_ = framesDC.SendText(string(payload))
	}
	return nil
}

// readBase64FromPrompt prompts user for base64 input until blank line or END
func readBase64FromPrompt(prompt string) (string, error) {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	var b strings.Builder
	for {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
				return "", err
			}
			break
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.EqualFold(strings.TrimSpace(line), "END") {
			break
		}
		b.WriteString(strings.TrimSpace(line))
	}
	return b.String(), nil
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
