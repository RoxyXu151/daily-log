package mobilehost

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"daily-log/internal/mobileapi"
	"daily-log/internal/workspace"
)

const cookieName = "__Host-daily-log"
const pairingTTL = 10 * time.Minute
const sessionTTL = 12 * time.Hour

type Host struct {
	pairingHash [sha256.Size]byte
	pairingEnd  time.Time
	mu          sync.Mutex
	sessionHash [sha256.Size]byte
	sessionEnd  time.Time
	api         *http.ServeMux
	assets      http.Handler
}

func New(backend workspace.Backend, assets http.Handler) (*Host, string, error) {
	host := &Host{assets: assets, api: routes(backend)}
	code, err := host.IssueCode()
	return host, code, err
}

// IssueCode replaces the pending code. A successful pairing replaces the active session.
func (h *Host) IssueCode() (string, error) {
	code, err := randomToken(16)
	if err != nil {
		return "", err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pairingHash, h.pairingEnd = sha256.Sum256([]byte(code)), time.Now().Add(pairingTTL)
	return code, nil
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (h *Host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.TLS == nil {
		http.Error(w, "请使用 HTTPS", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodPost && !sameOrigin(r) {
		problem(w, http.StatusForbidden, "origin", "请求必须来自同一采集主机页面")
		return
	}
	switch {
	case r.URL.Path == "/pair" && r.Method == http.MethodPost:
		h.pair(w, r)
	case r.URL.Path == "/pair" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		pairPage(w, http.StatusOK, "")
	case r.URL.Path == "/api/logout" && r.Method == http.MethodPost:
		h.logout(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/"):
		if !h.authenticated(r) {
			problem(w, http.StatusUnauthorized, "session", mobileapi.ErrSession.Error())
			return
		}
		h.api.ServeHTTP(w, r)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		if h.authenticated(r) {
			h.assets.ServeHTTP(w, r)
		} else if r.URL.Path == "/" || r.URL.Path == "/pair" {
			pairPage(w, http.StatusOK, "")
		} else {
			http.NotFound(w, r)
		}
	default:
		http.Error(w, "请求方法无效", http.StatusMethodNotAllowed)
	}
}

func sameOrigin(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, r.Host) &&
		u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}

func (h *Host) pair(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := r.ParseForm(); err != nil {
		pairPage(w, http.StatusBadRequest, "配对请求格式无效")
		return
	}
	input := sha256.Sum256([]byte(strings.TrimSpace(r.PostForm.Get("code"))))
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if !now.Before(h.pairingEnd) || subtle.ConstantTimeCompare(input[:], h.pairingHash[:]) != 1 {
		pairPage(w, http.StatusUnauthorized, "配对码无效或已使用，请在采集主机获取新配对码")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		http.Error(w, "无法创建会话", http.StatusInternalServerError)
		return
	}
	h.pairingEnd = time.Time{}
	h.sessionHash, h.sessionEnd = sha256.Sum256([]byte(token)), now.Add(sessionTTL)
	http.SetCookie(w, cookie(token, int(sessionTTL.Seconds())))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Host) authenticated(r *http.Request) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.matchesSession(r)
}

func (h *Host) matchesSession(r *http.Request) bool {
	session, err := r.Cookie(cookieName)
	if err != nil || len(session.Value) != base64.RawURLEncoding.EncodedLen(32) {
		return false
	}
	input := sha256.Sum256([]byte(session.Value))
	return time.Now().Before(h.sessionEnd) && subtle.ConstantTimeCompare(input[:], h.sessionHash[:]) == 1
}

func (h *Host) logout(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	valid := h.matchesSession(r)
	if valid {
		h.sessionEnd = time.Time{}
	}
	h.mu.Unlock()
	if !valid {
		problem(w, http.StatusUnauthorized, "session", mobileapi.ErrSession.Error())
		return
	}
	http.SetCookie(w, cookie("", -1))
	jsonResponse(w, http.StatusOK, struct{}{})
}

func cookie(value string, age int) *http.Cookie {
	return &http.Cookie{Name: cookieName, Value: value, Path: "/", MaxAge: age,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}

var pairingPage = template.Must(template.New("pair").Parse(`<!doctype html>
<html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>连接 daily-log</title>
<style>
:root{font:16px system-ui;color-scheme:light dark}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:1rem;box-sizing:border-box}
main{max-width:28rem;padding:1.6rem;border:1px solid GrayText;border-radius:1.2rem}
form{display:grid;gap:.75rem}input,button{font:inherit;padding:.75rem;border-radius:.5rem}button{cursor:pointer}
</style><main><h1>连接 daily-log</h1><p>请输入采集主机显示的一次性配对码。</p>
{{if .}}<p role="alert">{{.}}</p>{{end}}
<form method="post" action="/pair"><label for="code">配对码</label>
<input id="code" name="code" type="password" required spellcheck="false" autocapitalize="off" autocomplete="off">
<button type="submit">连接主机</button></form></main></html>`))

func pairPage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = pairingPage.Execute(w, message)
}

var errDesktopOnly = errors.New("此项涉及采集主机上的文件、命令或连接地址，请在桌面端设置")
