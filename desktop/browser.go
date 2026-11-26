//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"log"
	"syscall/js"

	"daily-log/internal/mobileapi"
	"daily-log/internal/model"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const appearanceKey = "daily-log-appearance"

func main() {
	location, document := js.Global().Get("location"), js.Global().Get("document")
	viewport := document.Call("querySelector", `meta[name="viewport"]`)
	if viewport.Truthy() {
		viewport.Call("setAttribute", "content", "width=device-width, initial-scale=1, interactive-widget=resizes-content")
	}
	api := mobileapi.Client{BaseURL: location.Get("origin").String()}
	ctx := context.Background()
	s := &screen{
		backend: api, ctx: ctx, remote: true, host: location.Get("host").String(),
		openLink:   mygo.Shell.OpenExternal,
		appearance: browserAppearance(),
		saveAppearance: func(p appearance) error {
			data, err := model.PrettyJSON(p)
			if err != nil {
				return err
			}
			return localStorage(func(storage js.Value) {
				storage.Call("setItem", appearanceKey, string(data))
			})
		},
	}
	s.reconnect = func() { js.Global().Call("open", "/pair", "_blank", "noopener") }
	s.disconnect = func() {
		s.perform("断开采集主机…", func() (func(), error) {
			err := api.Logout(ctx)
			return func() {
				if err == nil {
					location.Call("reload")
				}
			}, err
		})
	}
	var unbindTouch func()
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{Title: "daily-log · 移动工作台", Content: ui.View(s.view)})
		unbindTouch = bindTouchScroll(document.Call("querySelector", ".mygo-window > canvas"))
		s.dispatch = win.Update
		s.refresh()
	})
	if err := mygo.App.Run(); err != nil {
		log.Print(err)
	}
	if unbindTouch != nil {
		unbindTouch()
	}
}

func browserAppearance() appearance {
	p := defaultAppearance()
	_ = localStorage(func(storage js.Value) {
		item := storage.Call("getItem", appearanceKey)
		if item.Type() == js.TypeString {
			p, _ = decodeAppearance([]byte(item.String()))
		}
	})
	return p
}

func localStorage(fn func(js.Value)) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("浏览器无法访问本地外观设置")
		}
	}()
	fn(js.Global().Get("localStorage"))
	return nil
}
