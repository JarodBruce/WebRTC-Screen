//go:build !windows

package main

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed index.html
var embeddedFiles embed.FS

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	b, err := embeddedFiles.ReadFile("index.html")
	if err != nil {
		http.Error(w, "index missing", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(b)
}

func runServer(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// UDP signaling parameters (ENV)
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
	// New simple variables: set PEER_IP and UDP_PORT; others optional
	udpPort := getEnv("UDP_PORT", "8080")
	peerIP := getEnv("PEER_IP", "192.168.1.16")
	localhostOnly := getEnvBool("BIND_LOCALHOST_ONLY", false)
	// Derive bind/remote with overrides
	bindAddr := os.Getenv("LOCAL_ADDR")
	if bindAddr == "" {
		if localhostOnly {
			bindAddr = net.JoinHostPort("127.0.0.1", udpPort)
		} else {
			bindAddr = net.JoinHostPort("0.0.0.0", udpPort)
		}
	}
	remoteAddrStr := os.Getenv("REMOTE_ADDR")
	if remoteAddrStr == "" {
		remoteAddrStr = net.JoinHostPort(peerIP, udpPort)
	}

	// Prepare UDP socket
	localAddr, err := net.ResolveUDPAddr("udp4", bindAddr)
	if err != nil {
		log.Printf("UDP resolve local error: %v", err)
	}
	remoteAddr, err := net.ResolveUDPAddr("udp4", remoteAddrStr)
	if err != nil {
		log.Printf("UDP resolve remote error: %v", err)
	}
	conn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		log.Printf("UDP listen error: %v", err)
	}
	// /signal handler: accept offer (base64 or JSON), forward to Windows via UDP, return answer as JSON
	mux.HandleFunc("/signal", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			offerB64 string
			Offer    json.RawMessage
		}
		// Accept either raw base64 string or JSON object
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		// Try to parse as JSON first
		var tmp map[string]any
		if err := json.Unmarshal(body, &tmp); err == nil {
			if v, ok := tmp["offerB64"].(string); ok && v != "" {
				req.offerB64 = v
			} else if v, ok := tmp["offer"].(map[string]any); ok {
				req.Offer, _ = json.Marshal(v)
			}
		} else {
			// Raw body as base64
			req.offerB64 = strings.TrimSpace(string(body))
		}
		if len(req.Offer) == 0 && req.offerB64 == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("missing offer"))
			return
		}
		if len(req.Offer) != 0 && req.offerB64 == "" {
			req.offerB64 = base64.StdEncoding.EncodeToString(req.Offer)
		}
		if conn == nil || remoteAddr == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("UDP not configured"))
			return
		}
		// Send OFFER via UDP and wait for ANSWER
		_, _ = conn.WriteToUDP([]byte("OFFER:"+req.offerB64), remoteAddr)
		answerStr, _, err := waitForPrefix(conn, "ANSWER:", 30*time.Second)
		if err != nil {
			w.WriteHeader(http.StatusGatewayTimeout)
			_, _ = w.Write([]byte("wait ANSWER timeout"))
			return
		}
		// Decode and return JSON
		b, err := base64.StdEncoding.DecodeString(answerStr)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("invalid ANSWER b64"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})

	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		log.Println("http server started on", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("ListenAndServe:", err)
		}
	}()

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

// waitForPrefix reads UDP packets until one starting with the given prefix arrives, or timeout.
func waitForPrefix(conn *net.UDPConn, prefix string, timeout time.Duration) (string, *net.UDPAddr, error) {
	if conn == nil {
		return "", nil, fmt.Errorf("no UDP conn")
	}
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
	}
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	runServer(addr)
}
