//go:build js && wasm

package main

import (
	"math"
	"syscall/js"
)

// MyGo's web canvas handles taps and wheel scrolling. Route a one-finger
// vertical drag to the same scroll area, and cancel the click under the drag.
func bindTouchScroll(canvas js.Value) func() {
	if !canvas.Truthy() {
		return func() {}
	}
	var tracking, scrolling bool
	var pointer int
	var startX, startY, lastY float64
	global := js.Global()
	listeners := map[string]js.Func{}
	add := func(name string, handle func(js.Value)) {
		listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
			event := args[0]
			if event.Get("isTrusted").Bool() && event.Get("pointerType").String() == "touch" && event.Get("isPrimary").Bool() {
				handle(event)
			}
			return nil
		})
		listeners[name] = listener
		canvas.Call("addEventListener", name, listener, true)
	}
	add("pointerdown", func(event js.Value) {
		tracking, scrolling = true, false
		pointer = event.Get("pointerId").Int()
		startX, startY = event.Get("clientX").Float(), event.Get("clientY").Float()
		lastY = startY
	})
	add("pointermove", func(event js.Value) {
		if !tracking || event.Get("pointerId").Int() != pointer {
			return
		}
		y := event.Get("clientY").Float()
		if !scrolling {
			dx, dy := math.Abs(event.Get("clientX").Float()-startX), math.Abs(y-startY)
			if dy < 12 || dy < dx {
				return
			}
			scrolling = true
			cancel := global.Get("PointerEvent").New("pointercancel", map[string]any{
				"pointerId": pointer, "pointerType": "touch", "isPrimary": true, "clientX": startX, "clientY": startY,
			})
			canvas.Call("dispatchEvent", cancel)
		}
		event.Call("stopImmediatePropagation")
		wheel := global.Get("WheelEvent").New("wheel", map[string]any{
			"clientX": startX, "clientY": startY, "deltaY": lastY - y, "cancelable": true,
		})
		lastY = y
		canvas.Call("dispatchEvent", wheel)
	})
	add("pointerup", func(event js.Value) {
		if tracking && event.Get("pointerId").Int() == pointer {
			if scrolling {
				event.Call("stopImmediatePropagation")
			}
			tracking = false
		}
	})
	add("pointercancel", func(event js.Value) {
		if event.Get("pointerId").Int() == pointer {
			tracking = false
		}
	})
	return func() {
		for event, listener := range listeners {
			canvas.Call("removeEventListener", event, listener, true)
			listener.Release()
		}
	}
}
