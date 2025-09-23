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
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

func main() {
	answerFilePath := flag.String("answer-file", "answer.txt", "Path to the answer file")
	flag.Parse()

	// --- WebRTC PeerConnectionのセットアップ ---
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:1932"}},
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
	videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	if err != nil {
		panic(err)
	}
	rtpSender, err := peerConnection.AddTrack(videoTrack)
	if err != nil {
		panic(err)
	}
	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

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
	fmt.Println(Encode(*peerConnection.LocalDescription()))
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
		if s == webrtc.PeerConnectionStateConnected {
			fmt.Println("Connection established! Starting screen capture...")
			// --- 画面キャプチャとビデオ送信のループ ---
			go func() {
				ticker := time.NewTicker(33 * time.Millisecond) // ~30 FPS
				for range ticker.C {
					bounds := screenshot.GetDisplayBounds(0)
					img, err := screenshot.CaptureRect(bounds)
					if err != nil {
						// fmt.Println("Error capturing screen:", err)
						continue
					}

					// image.RGBA を image.YCbCr に変換
					ycbcrImg := toYCbCr(img)

					// WriteSampleに渡すデータはYCbCrのYプレーン（輝度）のみで良い場合がある
					// VP8エンコーダは内部で色差情報も扱うが、APIとしては輝度プレーンのバイトスライスを渡す
					if err := videoTrack.WriteSample(media.Sample{Data: ycbcrImg.Y, Duration: time.Second / 30}); err != nil {
						// fmt.Println("Error writing sample:", err)
					}
				}
			}()
		}
	})

	// アプリケーションが終了しないように待機
	select {}
}

// toYCbCr converts image.RGBA to image.YCbCr.
func toYCbCr(img *image.RGBA) *image.YCbCr {
	bounds := img.Bounds()
	ycbcr := image.NewYCbCr(bounds, image.YCbCrSubsampleRatio420)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)

			Y := 0.299*float64(r8) + 0.587*float64(g8) + 0.114*float64(b8)
			Cb := 128 - 0.168736*float64(r8) - 0.331264*float64(g8) + 0.5*float64(b8)
			Cr := 128 + 0.5*float64(r8) - 0.418688*float64(g8) - 0.081312*float64(b8)

			yi := y*ycbcr.YStride + x
			ci := (y/2)*ycbcr.CStride + (x / 2)

			ycbcr.Y[yi] = uint8(Y)
			if x%2 == 0 && y%2 == 0 {
				ycbcr.Cb[ci] = uint8(Cb)
				ycbcr.Cr[ci] = uint8(Cr)
			}
		}
	}
	return ycbcr
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
