package tables

import (
	"context"
	"daily-log/internal/model"
	"daily-log/internal/netclient"
	"daily-log/internal/store"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const SheetsScope = "https://www.googleapis.com/auth/spreadsheets"

type Sheets struct {
	AutoLayout, ActivityColors bool
	ID                         string
	HTTP                       netclient.Client
	DB                         *store.DB
	Privacy                    model.Redactor
	BaseURL                    string
}
type sheetProperty struct {
	Title string `json:"title"`
	ID    int    `json:"sheetId"`
	Grid  struct {
		Rows    int `json:"rowCount"`
		Columns int `json:"columnCount"`
	} `json:"gridProperties"`
}
type metadata struct {
	ID     string `json:"spreadsheetId"`
	Sheets []struct {
		Properties sheetProperty `json:"properties"`
	} `json:"sheets"`
}
type valueRange struct {
	Range  string  `json:"range"`
	Values [][]any `json:"values"`
}

func column(i int) string {
	v := ""
	for i > 0 {
		i--
		v = string(rune('A'+i%26)) + v
		i /= 26
	}
	return v
}
func a1(name string) string { return "'" + strings.ReplaceAll(name, "'", "''") + "'!" }
func (s Sheets) base() string {
	b := s.BaseURL
	if b == "" {
		b = "https://sheets.googleapis.com/v4/spreadsheets"
	}
	return strings.TrimRight(b, "/") + "/" + url.PathEscape(s.ID)
}
func (s Sheets) call(ctx context.Context, method, suffix string, params url.Values, body, result any) error {
	_, err := s.HTTP.JSON(ctx, method, s.base()+suffix, params, nil, body, result)
	return err
}
func (s Sheets) meta(ctx context.Context) (metadata, error) {
	var m metadata
	if s.ID == "" {
		return m, errors.New("请配置 sheets.spreadsheet_id")
	}
	err := s.call(ctx, http.MethodGet, "", url.Values{"fields": {"spreadsheetId,sheets.properties"}}, nil, &m)
	return m, err
}
func (s Sheets) Inspect(ctx context.Context) (model.Object, error) {
	m, err := s.meta(ctx)
	if err != nil {
		return nil, err
	}
	b, err := model.CanonicalJSON(m)
	if err != nil {
		return nil, err
	}
	var o model.Object
	err = model.Decode([]byte(b), &o)
	return o, err
}
func (s Sheets) values(ctx context.Context, ranges []string, render string) ([]valueRange, error) {
	var all []valueRange
	for len(ranges) > 0 {
		n := min(50, len(ranges))
		var r struct {
			Values []valueRange `json:"valueRanges"`
		}
		err := s.call(ctx, "GET", "/values:batchGet", url.Values{"ranges": ranges[:n], "valueRenderOption": {render}}, nil, &r)
		if err != nil {
			return nil, err
		}
		if len(r.Values) != n {
			return nil, errors.New("Sheets 批量读取返回范围不完整；未提交快照")
		}
		all = append(all, r.Values...)
		ranges = ranges[n:]
	}
	return all, nil
}
func (s Sheets) read(ctx context.Context, m metadata) (map[string]Table, error) {
	properties := map[string]sheetProperty{}
	for _, sh := range m.Sheets {
		properties[sh.Properties.Title] = sh.Properties
	}
	tables := map[string]Table{}
	for _, name := range Names {
		prop, ok := properties[name]
		if !ok || prop.Grid.Rows < 1 || prop.Grid.Columns < 1 {
			return nil, fmt.Errorf("既有工作簿缺少 %s 或有效网格；未写入", name)
		}
		t := Table{}
		ranges := []string{}
		for start := 1; start <= prop.Grid.Rows; start += 500 {
			ranges = append(ranges, fmt.Sprintf("%sA%d:%s%d", a1(name), start, column(prop.Grid.Columns), min(start+499, prop.Grid.Rows)))
		}
		blocks, err := s.values(ctx, ranges, "FORMULA")
		if err != nil {
			return nil, err
		}
		for i, b := range blocks {
			for len(t.Rows) < i*500 {
				t.Rows = append(t.Rows, nil)
			}
			t.Rows = append(t.Rows, b.Values...)
		}
		if name == Names[0] && len(t.Rows) > 0 {
			c := 0
			for i, h := range t.Rows[0] {
				if h == "日期" {
					c = i
				}
			}
			if prop.Grid.Rows > 1 {
				keys, err := s.values(ctx, []string{fmt.Sprintf("%s%s2:%s%d", a1(name), column(c+1), column(c+1), prop.Grid.Rows)}, "UNFORMATTED_VALUE")
				if err != nil {
					return nil, err
				}
				for _, r := range keys[0].Values {
					v := any("")
					if len(r) > 0 {
						v = r[0]
					}
					t.Keys = append(t.Keys, v)
				}
			}
		}
		tables[name] = t
	}
	return tables, nil
}
func (s Sheets) Write(ctx context.Context) (model.Object, error) {
	m, err := s.meta(ctx)
	if err != nil {
		return nil, err
	}
	before, err := s.read(ctx, m)
	if err != nil {
		return nil, err
	}
	data, err := Data(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	plan, err := Build(ctx, s.DB, s.base(), before, data, s.Privacy)
	if err != nil {
		return nil, err
	}
	// Re-read every row to be modified, including blank append locations.
	ranges := []string{}
	for _, r := range plan.Rows {
		ranges = append(ranges, fmt.Sprintf("%sA%d:%s%d", a1(r.Sheet), r.Number+1, column(len(r.Before)), r.Number+1))
	}
	// Grow grids before reading append rows that lie beyond existing dimensions.
	requests := []any{}
	for _, sh := range m.Sheets {
		prop := sh.Properties
		required := prop.Grid.Rows
		for _, r := range plan.Rows {
			if r.Sheet == prop.Title {
				required = max(required, r.Number+1)
			}
		}
		if required > prop.Grid.Rows {
			requests = append(requests, model.Object{"appendDimension": model.Object{"sheetId": prop.ID, "dimension": "ROWS", "length": required - prop.Grid.Rows + 200}})
		}
	}
	if len(requests) > 0 {
		var out model.Object
		if err = s.call(ctx, "POST", ":batchUpdate", nil, model.Object{"requests": requests}, &out); err != nil {
			return nil, err
		}
	}
	live, err := s.values(ctx, ranges, "FORMULA")
	if err != nil {
		return nil, err
	}
	for i, b := range live {
		fresh := []any{}
		if len(b.Values) > 0 {
			fresh = b.Values[0]
		}
		r := plan.Rows[i]
		if !equal(padded(fresh, len(r.Before)), r.Before) {
			return nil, errors.New("Sheets 读取后行内容发生变化，请重试；未写入单元格")
		}
	}
	// Bound each request and always use RAW so source text cannot become formulas.
	for start := 0; start < len(plan.Changes); start += 500 {
		entries := []any{}
		for _, ch := range plan.Changes[start:min(start+500, len(plan.Changes))] {
			entries = append(entries, model.Object{"range": fmt.Sprintf("%s%s%d", a1(ch.Sheet), column(ch.Column+1), ch.Row+1), "values": [][]any{{ch.Value}}})
		}
		var out model.Object
		if err = s.call(ctx, "POST", "/values:batchUpdate", nil, model.Object{"valueInputOption": "RAW", "data": entries}, &out); err != nil {
			return nil, err
		}
	}
	readback, err := s.values(ctx, ranges, "FORMULA")
	if err != nil {
		return nil, err
	}
	after := map[string]Table{}
	for i, b := range readback {
		r := plan.Rows[i]
		t := after[r.Sheet]
		for len(t.Rows) <= r.Number {
			t.Rows = append(t.Rows, nil)
		}
		if len(b.Values) > 0 {
			t.Rows[r.Number] = b.Values[0]
		}
		after[r.Sheet] = t
	}
	if err = plan.Verify(after); err != nil {
		return nil, err
	}
	if err = plan.Commit(ctx, s.DB); err != nil {
		return nil, err
	}
	if s.AutoLayout || s.ActivityColors {
		freshMeta, err := s.meta(ctx)
		if err != nil {
			return nil, err
		}
		fresh, err := s.read(ctx, freshMeta)
		if err != nil {
			return nil, err
		}
		if err = s.layout(ctx, freshMeta, fresh, data); err != nil {
			return nil, err
		}
	}
	plan.Report["verified_cells"] = len(plan.Changes)
	return plan.Report, nil
}
