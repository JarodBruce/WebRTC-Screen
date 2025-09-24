package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	_ "github.com/pion/mediadevices/pkg/driver/screen"
	"github.com/pion/webrtc/v4"
)

const maxUDPPayloadSize = 1200 // Safe payload size to avoid MTU issues

// writeFragments splits a message and sends it as UDP fragments.
func writeFragments(conn *net.UDPConn, addr *net.UDPAddr, msgID, message string) {
	numChunks := (len(message) + maxUDPPayloadSize - 1) / maxUDPPayloadSize
	for i := 0; i < numChunks; i++ {
		start := i * maxUDPPayloadSize
		end := start + maxUDPPayloadSize
		if end > len(message) {
			end = len(message)
		}
		chunkData := message[start:end]
		header := fmt.Sprintf("FRAG:%s:%d:%d:", msgID, numChunks, i)
		chunk := header + chunkData
		_, err := conn.WriteToUDP([]byte(chunk), addr)
		if err != nil {
			// In a real app, more robust error handling/retries would be needed.
			fmt.Printf("failed to write UDP chunk: %v\n", err)
		}
		// A small delay can help prevent overwhelming the receiver's buffer.
		time.Sleep(1 * time.Millisecond)
	}
}

// waitForMessageWithFragments waits for a message, reassembling if fragmented.
func waitForMessageWithFragments(conn *net.UDPConn, prefix string, timeout time.Duration) (string, *net.UDPAddr, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))

	chunks := make(map[string]map[int]string) // msgID -> chunkIndex -> data
	totalChunks := make(map[string]int)       // msgID -> total

	// Buffer needs to be large enough for a full chunk + headers
	buf := make([]byte, maxUDPPayloadSize+100)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return "", nil, err
		}

		msg := string(buf[:n])
		if strings.HasPrefix(msg, "FRAG:") {
			parts := strings.SplitN(msg, ":", 5)
			if len(parts) < 5 {
				continue // Malformed fragment
			}
			msgID, numChunksStr, chunkIndexStr, payload := parts[1], parts[2], parts[3], parts[4]

			numChunks, err := strconv.Atoi(numChunksStr)
			if err != nil {
				continue
			}
			chunkIndex, err := strconv.Atoi(chunkIndexStr)
			if err != nil {
				continue
			}

			if _, ok := chunks[msgID]; !ok {
				chunks[msgID] = make(map[int]string)
				totalChunks[msgID] = numChunks
			}
			chunks[msgID][chunkIndex] = payload

			// Check if we have all chunks for this message ID
			if len(chunks[msgID]) == totalChunks[msgID] {
				var reassembled strings.Builder
				// Sort by chunk index to ensure correct order
				keys := make([]int, 0, len(chunks[msgID]))
				for k := range chunks[msgID] {
					keys = append(keys, k)
				}
				sort.Ints(keys)
				for _, k := range keys {
					reassembled.WriteString(chunks[msgID][k])
				}

				fullMsg := reassembled.String()
				if strings.HasPrefix(fullMsg, prefix) {
					// Cleanup memory for this message ID
					delete(chunks, msgID)
					delete(totalChunks, msgID)
					return strings.TrimPrefix(fullMsg, prefix), addr, nil
				}
			}
		} else if strings.HasPrefix(msg, prefix) {
			// Handle non-fragmented message for simple cases
			return strings.TrimPrefix(msg, prefix), addr, nil
		}
	}
}

func main() {
	localAddr := flag.String("local-addr", ":8080", "Local UDP address to listen on")
	remoteAddr := flag.String("remote-addr", "127.0.0.1:8081", "Remote UDP address for the controller")
	flag.Parse()

	// --- UDP Signaling Setup ---
	laddr, err := net.ResolveUDPAddr("udp", *localAddr)
	if err != nil {
		panic(err)
	}
	raddr, err := net.ResolveUDPAddr("udp", *remoteAddr)
	if err != nil {
		panic(err)
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	fmt.Printf("Host listening for signaling on %s\n", conn.LocalAddr())
	fmt.Printf("Will send offer to controller at %s\n", raddr)

	// --- WebRTC PeerConnectionのセットアップ ---
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		panic(err)
	}
	defer func() {
		if cErr := peerConnection.Close(); cErr != nil {
			fmt.Printf("cannot close peerConnection: %v\n", cErr)
		}
	}()

	// --- Create video track from screen capture using mediadevices ---
	vpxParams, err := vpx.NewVP8Params()
	if err != nil {
		panic(err)
	}
	vpxParams.BitRate = 1_000_000 // 1Mbps

	codecSelector := mediadevices.NewCodecSelector(
		mediadevices.WithVideoEncoders(&vpxParams),
	)

	mediaStream, err := mediadevices.GetDisplayMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			// Specific video constraints can be set here if needed
		},
		Codec: codecSelector,
	})
	if err != nil {
		panic(err)
	}

	track := mediaStream.GetVideoTracks()[0]
	_, err = peerConnection.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendonly,
	})
	if err != nil {
		panic(err)
	}

	// --- シグナリング (オファーの生成とアンサーの待機) ---
	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		panic(err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	if err = peerConnection.SetLocalDescription(offer); err != nil {
		panic(err)
	}
	<-gatherComplete

	// --- シグナリング (オファーの送信とアンサーの待機) ---
	fmt.Println("--- Sending Offer via UDP ---")
	offerPayload := Encode(*peerConnection.LocalDescription())
	// Use a random ID for the message fragments
	msgID := fmt.Sprintf("%d", rand.Intn(100000))
	writeFragments(conn, raddr, msgID, "OFFER:"+offerPayload)
	fmt.Println("-----------------------------")

	fmt.Println("Waiting for Answer from controller...")
	answerStr, from, err := waitForMessageWithFragments(conn, "ANSWER:", 60*time.Second)
	if err != nil {
		panic(fmt.Sprintf("Error waiting for answer: %v", err))
	}
	fmt.Printf("Received Answer from %s\n", from)

	answer := webrtc.SessionDescription{}
	Decode(answerStr, &answer)

	if err = peerConnection.SetRemoteDescription(answer); err != nil {
		panic(err)
	}

	// --- 接続状態の監視 ---
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State has changed: %s\n", s.String())
		if s == webrtc.PeerConnectionStateFailed {
			fmt.Println("Peer Connection has failed. Exiting")
			os.Exit(0)
		}
		if s == webrtc.PeerConnectionStateConnected {
			fmt.Println("Connection established! Starting screen capture...")
			// Screen capture is handled by mediadevices, no need for a separate goroutine here.
		}
	})

	// アプリケーションが終了しないように待機
	select {}
}

// --- シグナリング情報 (SDP) をエンコード/デコードするためのヘルパー関数 ---

func Encode(obj interface{}) string {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func Decode(in string, obj interface{}) {
	b, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(b, obj)
	if err != nil {
		panic(err)
	}
}
