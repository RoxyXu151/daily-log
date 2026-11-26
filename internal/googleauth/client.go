package googleauth

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/privatefs"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const CalendarScope = "https://www.googleapis.com/auth/calendar.readonly"

type Credentials struct {
	Root           string
	ServiceAccount string
	UserToken      string
}

func (c Credentials) Client(ctx context.Context, timeout time.Duration, scopes ...string) (*http.Client, error) {
	var tokens oauth2.TokenSource
	switch {
	case c.ServiceAccount != "":
		_, data, err := c.read(c.ServiceAccount)
		if err != nil {
			return nil, err
		}
		config, err := google.JWTConfigFromJSON(data, scopes...)
		if err != nil {
			return nil, errors.New("无法加载 Google 服务账号")
		}
		tokens = config.TokenSource(ctx)
	case c.UserToken != "":
		path, data, err := c.read(c.UserToken)
		if err != nil {
			return nil, err
		}
		var saved model.Object
		if err := model.Decode(data, &saved); err != nil {
			return nil, errors.New("无法解析 Google OAuth 凭证")
		}
		oauth := oauth2.Config{
			ClientID: saved.Text("client_id"), ClientSecret: saved.Text("client_secret"),
			Scopes: scopes, Endpoint: google.Endpoint,
		}
		if oauth.ClientID == "" || oauth.ClientSecret == "" || saved.Text("refresh_token") == "" {
			return nil, errors.New("Google OAuth 凭证缺少刷新所需字段")
		}
		// A legacy token file may omit the expiry. Refresh rather than treating
		// the token as permanently valid.
		expiry, _ := time.Parse(time.RFC3339Nano, saved.Text("expiry"))
		if expiry.IsZero() {
			expiry = time.Unix(1, 0)
		}
		token := &oauth2.Token{AccessToken: saved.Text("token"), RefreshToken: saved.Text("refresh_token"), Expiry: expiry}
		refresh := persistedToken{source: oauth.TokenSource(ctx, token), path: path, saved: saved}
		tokens = oauth2.ReuseTokenSource(token, &refresh)
	default:
		return nil, errors.New("未配置 Google 凭证，请配置 Google OAuth 或服务账号")
	}
	client := oauth2.NewClient(ctx, safeTokenSource{tokens})
	client.Timeout = timeout
	return client, nil
}

func (c Credentials) read(path string) (string, []byte, error) {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.Root, path)
	}
	if err := privatefs.Restrict(path); err != nil {
		return "", nil, errors.New("Google 凭证文件不可用")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, errors.New("无法读取 Google 凭证文件")
	}
	return path, data, nil
}

type safeTokenSource struct{ oauth2.TokenSource }

func (s safeTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.TokenSource.Token()
	if err != nil {
		return nil, errors.New("无法获取 Google 访问令牌")
	}
	return token, nil
}

type persistedToken struct {
	source oauth2.TokenSource
	path   string
	saved  model.Object
}

func (p *persistedToken) Token() (*oauth2.Token, error) {
	token, err := p.source.Token()
	if err != nil {
		return nil, err
	}
	p.saved["token"], p.saved["refresh_token"] = token.AccessToken, token.RefreshToken
	p.saved["expiry"] = token.Expiry.UTC().Format(time.RFC3339Nano)
	data, err := model.PrettyJSON(p.saved)
	if err != nil {
		return nil, err
	}
	if err := privatefs.Write(p.path, data); err != nil {
		return nil, err
	}
	return token, nil
}
