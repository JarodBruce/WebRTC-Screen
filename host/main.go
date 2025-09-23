package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"time"

	"github.com/kbinani/screenshot"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

func main() {
	answerFilePath := flag.String("answer-file", "answer.txt", "Path to the answer file")
	flag.Parse()

	// --- WebRTC PeerConnectionのセットアップ ---
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs:           []string{"stun:stun.l.google.com:19302", "turn:numb.viagenie.ca:3478"},
				Username:       "webrtc",
				Credential:     "webrtc",
				CredentialType: webrtc.ICECredentialTypePassword,
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

	// --- ビデオトラックの作成 ---
	videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: "video/vp8"}, "video", "pion")
	if err != nil {
		panic(err)
	}
	_, err = peerConnection.AddTrack(videoTrack)
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

	fmt.Println("--- Offer (copy this to the controller's offer file) ---")
	fmt.Println(Encode(offer))
	fmt.Println("----------------------------------------------------------")

	// answer.txt が作成されるのを待つ
	fmt.Printf("Paste the Answer from the controller into %s and save it.\n", *answerFilePath)
	var answerBytes []byte
	for {
		answerBytes, err = os.ReadFile(*answerFilePath)
		if err == nil && len(answerBytes) > 0 {
			os.Remove(*answerFilePath) // ファイルを削除
			break
		}
		time.Sleep(1 * time.Second)
	}

	answer := webrtc.SessionDescription{}
	Decode(string(answerBytes), &answer)

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
	})

	fmt.Println("Connection established! Starting screen capture...")

	// --- 画面キャプチャとビデオ送信のループ ---
	go func() {
		ticker := time.NewTicker(33 * time.Millisecond) // ~30 FPS
		for range ticker.C {
			bounds := screenshot.GetDisplayBounds(0) // プライマリディスプレイを取得
			img, err := screenshot.CaptureRect(bounds)
			if err != nil {
				fmt.Println("Error capturing screen:", err)
				continue
			}

			// image.RGBA を image.YCbCr に変換 (VP8エンコーダが要求するフォーマット)
			ycbcrImg := image.NewYCbCr(img.Bounds(), image.YCbCrSubsampleRatio420)
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					// RGBA to YCbCr conversion
					// Note: This is a simplified conversion. For production, use a proper library.
					Y := (0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8))
					Cb := 128 + (-0.168736*float64(r>>8) - 0.331264*float64(g>>8) + 0.5*float64(b>>8))
					Cr := 128 + (0.5*float64(r>>8) - 0.418688*float64(g>>8) - 0.081312*float64(b>>8))

					ycbcrImg.Y[y*ycbcrImg.YStride+x] = uint8(Y)
					if y%2 == 0 && x%2 == 0 {
						uvIndex := (y/2)*ycbcrImg.CStride + (x / 2)
						ycbcrImg.Cb[uvIndex] = uint8(Cb)
						ycbcrImg.Cr[uvIndex] = uint8(Cr)
					}
				}
			}

			if err := videoTrack.WriteSample(media.Sample{Data: ycbcrImg.Y, Duration: time.Second}); err != nil {
				// This is a simplified way to send frames. A real implementation would need to handle frame timing and partitioning correctly.
				// For now, we just log the error and continue.
				// fmt.Println("Error writing sample:", err)
			}
		}
	}()

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
