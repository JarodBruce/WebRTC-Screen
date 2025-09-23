package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/mediadevices/pkg/frame"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v3"

	_ "github.com/pion/mediadevices/pkg/driver/screen" // screen driver
)

func main() {
	answerFilePath := flag.String("answer-file", "answer.txt", "Path to the answer file")
	flag.Parse()

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

	// --- ビデオトラックの作成 (mediadevicesを使用) ---
	vpxParams, err := vpx.NewVP8Params()
	if err != nil {
		panic(err)
	}
	vpxParams.BitRate = 500_000 // 500kbps

	codecSelector := mediadevices.NewCodecSelector(
		mediadevices.WithVideoEncoders(&vpxParams),
	)

	mediaStream, err := mediadevices.GetDisplayMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			c.FrameFormat = prop.FrameFormat(frame.FormatYUY2)
			c.Width = prop.Int(1280)
			c.Height = prop.Int(720)
		},
		Codec: codecSelector,
	})
	if err != nil {
		panic(err)
	}

	track, ok := mediaStream.GetVideoTracks()[0].(*mediadevices.VideoTrack)
	if !ok {
		panic("Track is not a video track")
	}
	_, err = peerConnection.AddTrack(track)
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
		if s == webrtc.PeerConnectionStateConnected {
			fmt.Println("Connection established! Starting screen capture...")
		}
	})

	// アプリケーションが終了しないように待機
	<-context.Background().Done()
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
