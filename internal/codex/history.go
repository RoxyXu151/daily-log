package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"time"

	"daily-log/internal/model"
)

type history struct {
	ctx     context.Context
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	in      io.WriteCloser
	lines   <-chan []byte
	id      int
	timeout time.Duration
}

func openHistory(ctx context.Context, argv []string, timeout time.Duration) (*history, error) {
	if len(argv) == 0 {
		return nil, errors.New("Codex 主机缺少只读命令")
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		cancel()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		cancel()
		return nil, errors.New("无法启动 Codex 历史接口")
	}
	lines := make(chan []byte)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 64<<10), 32<<20)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
	}()
	h := &history{ctx: ctx, cmd: cmd, cancel: cancel, in: in, lines: lines, timeout: timeout}
	var result model.Object
	err = h.call("initialize", model.Object{"clientInfo": model.Object{"name": "personal_daily_log", "version": "1.0.0"}}, &result)
	if err == nil {
		err = h.send(model.Object{"method": "initialized"})
	}
	if err != nil {
		h.close()
		return nil, err
	}
	return h, nil
}

func (h *history) close() {
	h.cancel()
	h.in.Close()
	h.cmd.Wait()
}

func (h *history) send(value model.Object) error {
	if err := json.NewEncoder(h.in).Encode(value); err != nil {
		return errors.New("Codex 会话读取连接已关闭")
	}
	return nil
}

func (h *history) call(method string, params model.Object, result any) error {
	h.id++
	if err := h.send(model.Object{"id": h.id, "method": method, "params": params}); err != nil {
		return err
	}
	timer := time.NewTimer(h.timeout)
	defer timer.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return h.ctx.Err()
		case <-timer.C:
			return errors.New("Codex 会话读取超时")
		case line, open := <-h.lines:
			if !open {
				return errors.New("Codex 会话读取连接已关闭")
			}
			var response struct {
				ID     json.RawMessage
				Result json.RawMessage
				Error  json.RawMessage
			}
			if err := model.Decode(line, &response); err != nil {
				return errors.New("Codex 历史接口返回无效数据")
			}
			var id int
			if model.Decode(response.ID, &id) != nil || id != h.id {
				continue
			}
			if len(response.Error) > 0 || model.Decode(response.Result, result) != nil {
				return errors.New("Codex 历史接口读取失败，请检查 CLI 版本与连接")
			}
			return nil
		}
	}
}

func (h *history) pages(method string, params model.Object, consume func([]model.Object) (bool, error)) error {
	seen := map[string]bool{}
	params["cursor"] = nil
	for {
		var response struct {
			Data       []model.Object
			NextCursor string
		}
		if err := h.call(method, params, &response); err != nil {
			return err
		}
		stop, err := consume(response.Data)
		if stop || err != nil || response.NextCursor == "" {
			return err
		}
		if seen[response.NextCursor] {
			return errors.New("Codex 分页游标重复")
		}
		seen[response.NextCursor] = true
		params["cursor"] = response.NextCursor
	}
}

// CheckHistory verifies the read-only connection without collecting turns or
// sending conversation content to a model.
func CheckHistory(ctx context.Context, host Host, timeout time.Duration) error {
	client, err := openHistory(ctx, host.Command, timeout)
	if err != nil {
		return err
	}
	defer client.close()
	var result model.Object
	return client.call("thread/list", model.Object{
		"limit": 1, "sortKey": "updated_at", "sortDirection": "desc",
		"modelProviders": []string{}, "sourceKinds": []string{"vscode", "appServer"},
		"useStateDbOnly": true,
	}, &result)
}
