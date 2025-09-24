package main

import (
	"log"
	"os"
	"strings"
)

func main() {
	// Config for server defaults
	addr := ":8080" // for serving index.html on Mac
	fps := 10
	quality := 80
	display := 0

	mode := strings.ToLower(os.Getenv("MODE")) // "server" on Mac, "peer" on Windows
	if mode == "" {
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
