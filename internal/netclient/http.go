package netclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"daily-log/internal/model"
)

type Client struct {
	HTTP     *http.Client
	Attempts int
	Backoff  time.Duration
}

type StatusError int

func (e StatusError) Error() string { return fmt.Sprintf("API 返回 HTTP %d", e) }

func (c Client) JSON(ctx context.Context, method, endpoint string, params url.Values, headers http.Header, body any, result any) (http.Header, error) {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	response, data, err := c.Request(ctx, method, endpoint, params, headers, encoded)
	if err != nil {
		return nil, err
	}
	if err := model.Decode(data, result); err != nil {
		return nil, errors.New("API 返回了无效的 JSON")
	}
	return response, nil
}

func (c Client) Request(ctx context.Context, method, endpoint string, params url.Values, headers http.Header, body []byte) (http.Header, []byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, nil, errors.New("API 地址无效")
	}
	query := u.Query()
	for key, value := range params {
		query[key] = value
	}
	u.RawQuery = query.Encode()
	var last error
	for attempt := 0; attempt < max(1, c.Attempts); attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
		if err != nil {
			return nil, nil, errors.New("无法创建 API 请求")
		}
		request.Header = make(http.Header)
		if headers != nil {
			request.Header = headers.Clone()
		}
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		delay := math.Min(60, c.Backoff.Seconds()*math.Pow(2, float64(attempt)))
		response, err := c.HTTP.Do(request)
		if err != nil {
			last = errors.New("网络连接失败，已保留待补采状态")
		} else {
			if response.StatusCode < 400 {
				const maxResponse = 64 << 20
				data, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
				response.Body.Close()
				if readErr == nil {
					if len(data) > maxResponse {
						return nil, nil, errors.New("API 响应超过 64 MiB")
					}
					return response.Header, data, nil
				}
				last = errors.New("API 响应读取中断")
			} else {
				response.Body.Close()
				last = StatusError(response.StatusCode)
				switch response.StatusCode {
				case 429:
					if seconds, err := strconv.ParseFloat(response.Header.Get("Retry-After"), 64); err == nil && !math.IsNaN(seconds) {
						delay = math.Min(60, math.Max(delay, seconds))
					}
				case 408, 500, 502, 503, 504:
				default:
					return nil, nil, last
				}
			}
		}
		if attempt+1 >= c.Attempts {
			break
		}
		timer := time.NewTimer(time.Duration(delay * float64(time.Second)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, nil, last
}
