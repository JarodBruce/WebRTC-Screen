//go:build darwin

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
