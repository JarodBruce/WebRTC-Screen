package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"math/rand"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/pion/webrtc/v4"
)

const (
	screenWidth       = 1280
	screenHeight      = 720
	maxUDPPayloadSize = 1200 // Safe payload size to avoid MTU issues
)

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
			fmt.Printf("failed to write UDP chunk: %v\n", err)
		}
		time.Sleep(1 * time.Millisecond)
	}
}

// waitForMessageWithFragments waits for a message, reassembling if fragmented.
func waitForMessageWithFragments(conn *net.UDPConn, prefix string, timeout time.Duration) (string, *net.UDPAddr, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))

	chunks := make(map[string]map[int]string) // msgID -> chunkIndex -> data
	totalChunks := make(map[string]int)       // msgID -> total

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
				continue
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

			if len(chunks[msgID]) == totalChunks[msgID] {
				var reassembled strings.Builder
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
					delete(chunks, msgID)
					delete(totalChunks, msgID)
					return strings.TrimPrefix(fullMsg, prefix), addr, nil
				}
			}
		} else if strings.HasPrefix(msg, prefix) {
			return strings.TrimPrefix(msg, prefix), addr, nil
		}
	}
}

// Game structはEbitenのゲーム状態を管理します
type Game struct {
	imgLock sync.Mutex
	img     *image.RGBA
}

// Updateはゲームのロジックを更新します (今回は入力イベントの送信に使う予定)
func (g *Game) Update() error {
	// このループでマウスやキーボードの入力を検知し、データチャネルで送信する
	return nil
}

// Drawは画面を描画します
func (g *Game) Draw(screen *ebiten.Image) {
	g.imgLock.Lock()
	defer g.imgLock.Unlock()
	if g.img != nil {
		screen.WritePixels(g.img.Pix)
	}
}

// Layoutは画面サイズを決定します
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func main() {
	localAddr := flag.String("local-addr", ":8081", "Local UDP address to listen on")
	flag.Parse()

	// --- UDP Signaling Setup ---
	laddr, err := net.ResolveUDPAddr("udp", *localAddr)
	if err != nil {
		panic(err)
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	fmt.Printf("Controller listening for signaling on %s\n", conn.LocalAddr())

	game := &Game{}

	// --- WebRTC PeerConnectionのセットアップ ---
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{URLs: []string{"stun:stun1.l.google.com:19302"}},
			{URLs: []string{"stun:stun2.l.google.com:19302"}},
			{URLs: []string{"stun:stun3.l.google.com:19302"}},
			{URLs: []string{"stun:stun4.l.google.com:19302"}},
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

	// --- ビデオトラックの受信設定 ---
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		fmt.Printf("Track has started, of type %d: %s \n", track.PayloadType(), track.Codec().MimeType)
		codecName := strings.Split(track.Codec().MimeType, "/")[1]
		fmt.Printf("Codec: %s\n", codecName)

		// 受信したビデオフレームをデコードしてebitenの画像に変換するゴルーチン
		go func() {
			// RTPパケットを結合してJPEGフレームを再構築するためのバッファ
			var jpegBuf []byte
			for {
				// RTPパケットを読み込む
				rtpPacket, _, readErr := track.ReadRTP()
				if readErr != nil {
					fmt.Println("Error reading RTP packet:", readErr)
					return
				}

				// パケットのペイロードをバッファに追加
				jpegBuf = append(jpegBuf, rtpPacket.Payload...)

				// RTPマーカービットが立っている場合、フレームの最後のパケットを示す
				if rtpPacket.Marker {
					// JPEGデータをデコード
					img, err := jpeg.Decode(bytes.NewReader(jpegBuf))
					if err != nil {
						// デコードエラーが発生した場合はバッファをクリアして次のフレームを待つ
						// fmt.Println("Error decoding jpeg:", err)
						jpegBuf = nil
						continue
					}

					// デコードされた画像をEbitenで表示できるRGBA形式に変換
					bounds := img.Bounds()
					rgba := image.NewRGBA(bounds)
					for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
						for x := bounds.Min.X; x < bounds.Max.X; x++ {
							rgba.Set(x, y, img.At(x, y))
						}
					}

					// Ebitenの画像データを更新
					game.imgLock.Lock()
					game.img = rgba
					game.imgLock.Unlock()

					// 次のフレームのためにバッファをリセット
					jpegBuf = nil
				}
			}
		}()
	})

	// --- シグナリング (オファーの待機とアンサーの生成) ---
	fmt.Println("Waiting for Offer from host...")
	offerStr, remoteAddr, err := waitForMessageWithFragments(conn, "OFFER:", 60*time.Second)
	if err != nil {
		panic(fmt.Sprintf("Error waiting for offer: %v", err))
	}
	fmt.Printf("Received Offer from %s\n", remoteAddr)

	offer := webrtc.SessionDescription{}
	Decode(offerStr, &offer)

	if err = peerConnection.SetRemoteDescription(offer); err != nil {
		panic(err)
	}

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		panic(err)
	}
	<-gatherComplete

	fmt.Println("--- Sending Answer via UDP ---")
	answerPayload := Encode(*peerConnection.LocalDescription())
	msgID := fmt.Sprintf("%d", rand.Intn(100000))
	writeFragments(conn, remoteAddr, msgID, "ANSWER:"+answerPayload)
	fmt.Println("------------------------------")

	// --- 接続状態の監視 ---
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State has changed: %s\n", s.String())
		if s == webrtc.PeerConnectionStateFailed {
			fmt.Println("Peer Connection has failed. Exiting")
			os.Exit(0)
		}
	})

	// --- Ebitenウィンドウの起動 ---
	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowTitle("WebLinuxGUI Controller")
	if err := ebiten.RunGame(game); err != nil {
		panic(err)
	}
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
