package export

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"daily-log/internal/model"
	"daily-log/internal/privatefs"
)

type journalReader interface {
	Journals(context.Context) ([]model.Record, error)
}

type Markdown struct {
	Directory string
	Records   journalReader
	Privacy   model.Redactor
}

func (m Markdown) Write(ctx context.Context) (model.Object, error) {
	if err := privatefs.Directory(m.Directory); err != nil {
		return nil, err
	}
	journals, err := m.Records.Journals(ctx)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, record := range journals {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := model.ParseDay(record.Day); err != nil {
			return nil, fmt.Errorf("数据库中存在无效日记日期：%w", err)
		}
		record.Payload = m.Privacy.Object(record.Payload)
		data, err := model.PrettyJSON(record.Payload)
		if err != nil {
			return nil, err
		}
		for _, file := range []struct {
			name string
			data []byte
		}{
			{record.Day + ".md", []byte(markdown(record))},
			{record.Day + ".json", data},
		} {
			changed, err := privatefs.WriteChanged(filepath.Join(m.Directory, file.name), file.data)
			if err != nil {
				return nil, err
			}
			if changed {
				count++
			}
		}
	}
	return model.Object{"directory": m.Directory, "written_files": count}, nil
}

func markdown(record model.Record) string {
	var body strings.Builder
	fmt.Fprintf(&body, "# %s 日志\n\n", record.Day)
	coverage := record.Payload.Object("coverage")
	missing := []string{}
	for source := range coverage {
		if coverage.Text(source) != "ok" {
			missing = append(missing, source)
		}
	}
	slices.Sort(missing)
	labels := map[string]string{"zotero": "Zotero", "todoist": "Todoist", "codex": "Codex", "calendar": "日历", "chatgpt": "ChatGPT"}
	for i, name := range missing {
		if label := labels[name]; label != "" {
			missing[i] = label
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(&body, "> 采集覆盖不完整：%s。以下仅列已取得的记录。\n\n", strings.Join(missing, "、"))
	}
	for _, slot := range []string{"上午", "下午", "晚上"} {
		fmt.Fprintf(&body, "## %s\n\n%s\n\n", slot, record.Payload.Text(slot))
	}
	if manual := record.Payload.Text("manual"); manual != "" {
		fmt.Fprintf(&body, "## 本人补充\n\n%s\n\n", manual)
	}
	return body.String()
}
