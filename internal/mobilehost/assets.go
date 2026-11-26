package mobilehost

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var wasmName = regexp.MustCompile(`^daily-log\.[0-9a-f]{12}\.wasm$`)

// Assets opens only files from the MyGo distribution root. An accidentally
// selected configuration directory must never become a file server.
func Assets(root string) (http.Handler, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	var wasmCount int
	for _, entry := range entries {
		name := entry.Name()
		wasm := wasmName.MatchString(name)
		switch {
		case wasm, name == "index.html", name == "wasm_exec.js", name == "icon.png", name == "icon.svg":
		default:
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("浏览器资源必须是普通文件")
		}
		files[name] = filepath.Join(root, name)
		if wasm {
			wasmCount++
		}
	}
	if wasmCount != 1 || files["index.html"] == "" || files["wasm_exec.js"] == "" {
		return nil, errors.New("浏览器资源不完整，请先构建 MyGo 的 web 目标")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		file := files[name]
		if file == "" {
			http.NotFound(w, r)
			return
		}
		if wasmName.MatchString(name) {
			w.Header().Set("Content-Type", "application/wasm")
		}
		http.ServeFile(w, r, file)
	}), nil
}
