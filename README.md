# WebRTC Remote Desktop (manual signaling)

This project streams the Windows desktop to a macOS browser via WebRTC DataChannels with manual copy/paste signaling. No relay or signaling servers are used. STUN uses Google's public server.

## Architecture

- macOS runs this binary in `MODE=server` to host `index.html` only. Open it in Safari/Chrome.
- Windows runs this binary in `MODE=peer`. It captures the screen and injects mouse/keyboard events.
- The browser creates the WebRTC Offer and two DataChannels:
  - `input` (browser -> Windows): input events JSON
  - `frames` (Windows -> browser): base64 JPEG frames JSON
- You copy/paste the SDP Offer/Answer between the browser and the Windows console.

## Prereqs

- Go 1.21+ (tested with 1.24 toolchain)
- Windows: a normal desktop session (not headless). App must be run with enough permissions to send input.

## Run

On macOS (serve the static page only):

```bash
go run ./client.go
# Then open http://localhost:8080 in your browser
```

In the page:

1. Click "1) Create Offer". Copy the base64 text from "Local Offer".

On Windows (the peer that streams desktop and injects input):

```powershell
go run ./windows.go
# You'll see a prompt:
#   Paste Offer (base64) from browser. End with an empty line or type END on a new line:
# Paste the Offer (base64). Finish by entering a blank line or typing END.
# The program will then print:
#   Answer (base64) — copy this back into the browser:
# Copy that entire base64 string back into the page's "Paste Answer" box and click "Set Answer".
```

Back on macOS browser:

1. Paste the Answer (base64) into "Paste Answer" and click "2) Set Answer".
2. The canvas should update with the remote desktop; your mouse/keyboard events are sent.

## Notes

- STUN: `stun:stun.l.google.com:19302` only. No TURN or signaling servers.
- Manual signaling via copy/paste means both endpoints must be able to reach each other peer-to-peer. If not, you may need a TURN server (intentionally not included per requirements).
- macOS build uses no-op input shims; input injection happens only on Windows.
- Windows peer optionally supports env overrides: `FPS` (default 10), `QUALITY` (default 80), `DISPLAY_INDEX` (default 0).
- FPS/quality are fixed in code (defaults: 10 FPS, JPEG quality 80). Adjust in `main.go` if needed.

## Troubleshooting

- If the connection doesn't establish, check firewall and NAT. Some networks block UDP.
- If you see no image, check the Windows console for errors and ensure the `frames` DataChannel opens.
- If you get Go build errors about `pion/webrtc`, run:
  ```bash
  go mod tidy
  ```
- If JPEG frames seem slow, try lowering quality or FPS in `main.go`.
