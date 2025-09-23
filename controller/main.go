package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/pion/webrtc/v3"
)

const (
	screenWidth  = 1280
	screenHeight = 720
)

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
	offerFilePath := flag.String("offer-file", "offer.txt", "Path to the offer file")
	flag.Parse()

	game := &Game{}

	// --- WebRTC PeerConnectionのセットアップ ---
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
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
			// ticker := new(time.Ticker) // This was unused
			// This is a very basic example of how to handle frames.
			// In a real-world application, you would want to use a more robust solution.
			// For example, you would want to handle packet loss and out-of-order packets.
			// You would also want to use a jitter buffer.
			for {
				_, _, readErr := track.ReadRTP()
				if readErr != nil {
					// handle error
					return // Exit goroutine on error
				}
				// Frame handling logic will be implemented here.
			}
		}()
	})

	// --- シグナリング (オファーの待機とアンサーの生成) ---
	fmt.Printf("Paste the Offer from the host into %s and save it.\n", *offerFilePath)
	var offerBytes []byte
	for {
		offerBytes, err = os.ReadFile(*offerFilePath)
		if err == nil && len(offerBytes) > 0 {
			os.Remove(*offerFilePath) // ファイルを削除
			break
		}
		time.Sleep(1 * time.Second)
	}

	offer := webrtc.SessionDescription{}
	Decode(string(offerBytes), &offer)

	if err = peerConnection.SetRemoteDescription(offer); err != nil {
		panic(err)
	}

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	// gatherComplete := webrtc.GatheringCompletePromise(peerConnection) // This was unused
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		panic(err)
	}
	// <-gatherComplete // This was unused

	fmt.Println("--- Answer (copy this to the host's answer file) ---")
	fmt.Println(Encode(*peerConnection.LocalDescription()))
	fmt.Println("----------------------------------------------------")

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
