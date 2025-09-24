//go:build darwin && cgo

package input

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

static void cgScroll(double dy) {
    // dy > 0 means scroll down in browser; CG expects positive to scroll up.
    // Invert sign to match typical expectations.
    double val = -dy;
    // CGEventCreateScrollWheelEvent uses fixed-point 16.16 for pixel mode levels in older APIs,
    // but kCGScrollEventUnitPixel accepts integer values representing pixels. We'll scale modestly.
    int32_t amount = (int32_t)(val);
    if (amount == 0) {
        amount = (val > 0) ? 1 : -1;
    }
    CGEventRef ev = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitPixel, 1, amount);
    if (ev) {
        CGEventPost(kCGHIDEventTap, ev);
        CFRelease(ev);
    }
}
*/
import "C"

func scroll(deltaY float64) {
	if deltaY == 0 {
		return
	}
	C.cgScroll(C.double(deltaY))
}

// The following are minimal no-op implementations on macOS. This project
// acts as the browser/server on macOS and controls a remote Windows host,
// so we do not inject local input here. These are provided to satisfy the
// cross-platform interface and keep builds green on darwin.

func moveMouse(x, y int) {}

func getMousePos() (int, int) { return 0, 0 }

func click(btn Button) {}

func keyDown(name string) {}

func keyUp(name string) {}

func typeString(s string) {}
