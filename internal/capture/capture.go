package capture

import (
	"bytes"
	"encoding/base64"
	"image/jpeg"
	"log"

	"github.com/go-vgo/robotgo"
	"github.com/kbinani/screenshot"
)

type Options struct {
	Display int
	Quality int // 1-100
}

// Frame captures the configured display and returns base64 JPEG and mouse coordinates
func Frame(opts Options) (imgB64 string, mouseX, mouseY int, err error) {
	bounds := screenshot.GetDisplayBounds(opts.Display)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return "", 0, 0, err
	}
	buf := new(bytes.Buffer)
	q := opts.Quality
	if q <= 0 || q > 100 {
		q = 80
	}
	if err = jpeg.Encode(buf, img, &jpeg.Options{Quality: q}); err != nil {
		return "", 0, 0, err
	}
	imgB64 = base64.StdEncoding.EncodeToString(buf.Bytes())
	x, y := robotgo.GetMousePos()
	return imgB64, x, y, nil
}

func LogIf(err error, msg string) bool {
	if err != nil {
		log.Println(msg, err)
		return true
	}
	return false
}
