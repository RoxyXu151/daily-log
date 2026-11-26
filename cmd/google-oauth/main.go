// google-oauth obtains a refresh token using a loopback callback and PKCE.
package main

import (
	"context"
	"crypto/rand"
	"daily-log/internal/googleauth"
	"daily-log/internal/model"
	"daily-log/internal/privatefs"
	"daily-log/internal/tables"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	client := flag.String("client", "secrets/client.json", "Google Desktop OAuth client JSON")
	output := flag.String("token", "secrets/google-token.json", "凭证输出路径")
	calendar := flag.Bool("include-calendar", false, "请求日历只读权限")
	flag.Parse()
	data, err := os.ReadFile(*client)
	if err != nil {
		return err
	}
	var document model.Object
	if err = model.Decode(data, &document); err != nil {
		return errors.New("OAuth 客户端 JSON 无效")
	}
	if document.Object("installed") == nil {
		return errors.New("需要桌面应用 OAuth 客户端（installed）")
	}
	scopes := []string{tables.SheetsScope}
	if *calendar {
		scopes = append(scopes, googleauth.CalendarScope)
	}
	config, err := google.ConfigFromJSON(data, scopes...)
	if err != nil {
		return errors.New("OAuth 客户端无效")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	config.RedirectURL = "http://" + listener.Addr().String() + "/callback"
	entropy := make([]byte, 32)
	if _, err = rand.Read(entropy); err != nil {
		return err
	}
	state := hex.EncodeToString(entropy)
	verifier := oauth2.GenerateVerifier()
	type response struct {
		code string
		err  error
	}
	done := make(chan response, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "授权状态不匹配", http.StatusBadRequest)
			return
		}
		value := response{code: r.URL.Query().Get("code")}
		if value.code == "" {
			value.err = errors.New("Google 授权未完成")
		}
		select {
		case done <- value:
		default:
		}
		fmt.Fprint(w, "授权回调已收到，请返回终端。")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case done <- response{err: errors.New("授权回调服务失败")}:
			default:
			}
		}
	}()
	fmt.Println("在浏览器打开以下地址完成授权（10 分钟内有效）：")
	fmt.Println(config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"), oauth2.S256ChallengeOption(verifier)))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	select {
	case <-ctx.Done():
		return errors.New("授权等待超时，请重新运行命令")
	case response := <-done:
		if response.err != nil {
			return response.err
		}
		token, err := config.Exchange(ctx, response.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return errors.New("无法交换 Google 授权令牌")
		}
		if token.RefreshToken == "" {
			return errors.New("Google 未返回刷新令牌，请重新授权")
		}
		saved := model.Object{"token": token.AccessToken, "refresh_token": token.RefreshToken, "client_id": config.ClientID, "client_secret": config.ClientSecret, "expiry": token.Expiry.UTC().Format(time.RFC3339Nano), "scopes": scopes}
		encoded, err := model.PrettyJSON(saved)
		if err != nil {
			return err
		}
		if err = privatefs.Write(*output, encoded); err != nil {
			return err
		}
		fmt.Println("已保存凭证：", *output)
		return nil
	}
}
