Multi-connection streaming (server notes)

This repository's server supports splitting the screen stream across multiple WebSocket/TCP connections to increase throughput.

How it works

- Control connection: open a websocket to `/ws` (default) or `/ws?role=control`. This connection sends input events (mouse/keyboard) to the server.
- Stream connections: open one or more websockets to `/ws?role=stream`. Each stream connection receives a subset of frames in round-robin order.

Client responsibilities

- Open 1 control connection and N stream connections (N>=1). For example:

  - ws://<host>:8080/ws?role=control
  - ws://<host>:8080/ws?role=stream
  - ws://<host>:8080/ws?role=stream

- The server will send JSON messages of the shape:
  {"image":"<base64-jpeg>","mouseX":123,"mouseY":456}

- Because frames are distributed across N stream sockets, the client must reassemble the stream in order for display. A simple approach:
  - Maintain a per-stream queue of received frames.
  - Present frames at a fixed display rate (e.g., 30 FPS) by pulling the next frame from the next stream in round-robin order.
  - If a stream has no queued frame, skip it.

Notes and caveats

- The current server uses a single global client for simplicity. For multiple simultaneous remote clients, the server needs a client registry keyed by a session ID and proper mutex protection.
- This approach increases throughput by using multiple TCP connections, which can avoid single-connection congestion and leverage parallelism. It's not a substitute for proper video streaming (WebRTC/RTSP) when low latency is required.
