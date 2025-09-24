package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"weblinuxgui/internal/clients"
	"weblinuxgui/internal/server"
)

func main() {
	// Fixed configuration (no flags/args as requested)
	addr := ":8080"
	fps := 10
	quality := 80
	display := 0

	if os.Getenv("DISPLAY") == "" {
		// Keep previous behavior if unset
		os.Setenv("DISPLAY", ":0")
	}

	mgr := clients.NewManager()

	mux := http.NewServeMux()
	mux.HandleFunc("/", server.ServeIndex)
	mux.HandleFunc("/ws", server.HandleWS(mgr))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: addr, Handler: mux}

	// Start streamer loop
	stopStream := server.StartStreamer(server.Config{FPS: fps, Quality: quality, Display: display, Manager: mgr})

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
