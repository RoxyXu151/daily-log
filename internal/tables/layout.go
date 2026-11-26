package tables

import (
	"context"
	"daily-log/internal/journal"
	"daily-log/internal/model"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode/utf16"
)

var palette = map[string]string{model.Research: "1967D2", model.Course: "137333", model.Career: "A05A00", model.Life: "8430A8"}

func rgb(hex string) model.Object {
	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return model.Object{"red": float64(r) / 255, "green": float64(g) / 255, "blue": float64(b) / 255}
}
func asObject(v any) model.Object {
	switch o := v.(type) {
	case map[string]any:
		return model.Object(o)
	case model.Object:
		return o
	}
	return model.Object{}
}
func colorRuns(text string, events []model.Event, old []any, defaultFormat model.Object) []any {
	colors := map[int]string{}
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		if line == "" {
			continue
		}
		trim := strings.TrimSpace(line)
		hex := palette[journal.LineCategory(trim, events)]
		for _, prefix := range []string{"暂无", "另有 ", "数量占比", "用时占比", "实际分钟"} {
			if strings.HasPrefix(trim, prefix) {
				hex = "3C4043"
			}
		}
		if hex == "" {
			hex = "3C4043"
		}
		colors[offset] = hex
		offset += len(utf16.Encode([]rune(line)))
	}
	originals := map[int]model.Object{0: defaultFormat}
	for _, v := range old {
		o := asObject(v)
		index, _ := numericValue(o["startIndex"])
		if int(index) < offset {
			originals[int(index)] = asObject(o["format"])
		}
	}
	set := map[int]bool{}
	for i := range colors {
		set[i] = true
	}
	for i := range originals {
		set[i] = true
	}
	points := []int{}
	for i := range set {
		points = append(points, i)
	}
	sort.Ints(points)
	format := model.Object{}
	color := "3C4043"
	runs := []any{}
	for _, i := range points {
		if o, ok := originals[i]; ok {
			format = o
		}
		if c, ok := colors[i]; ok {
			color = c
		}
		next := clone(format)
		delete(next, "foregroundColor")
		next["foregroundColorStyle"] = model.Object{"rgbColor": rgb(color)}
		runs = append(runs, model.Object{"startIndex": i, "format": next})
	}
	return runs
}
func (s Sheets) layout(ctx context.Context, m metadata, tables map[string]Table, data Dataset) error {
	if !s.AutoLayout && !s.ActivityColors {
		return nil
	}
	var prop sheetProperty
	for _, sh := range m.Sheets {
		if sh.Properties.Title == Names[0] {
			prop = sh.Properties
		}
	}
	t := tables[Names[0]]
	if len(t.Rows) == 0 {
		return nil
	}
	columns := []int{}
	keyCol := 0
	for c, h := range t.Rows[0] {
		h := fmt.Sprint(h)
		if a := aliases[h]; a != "" {
			h = a
		}
		if h == "日期" {
			keyCol = c
		}
		for _, owned := range Schemas[Names[0]][2:6] {
			if h == owned {
				columns = append(columns, c)
			}
		}
	}
	requests := []any{}
	ranges := []string{}
	rowDays := map[int]string{}
	for r := 1; r < len(t.Rows); r++ {
		v := padded(t.Rows[r], len(t.Rows[0]))[keyCol]
		if r-1 < len(t.Keys) {
			v = t.Keys[r-1]
		}
		day, err := normalize(v, true)
		if err != nil {
			return err
		}
		if _, ok := data[Names[0]][day]; !ok {
			continue
		}
		rowDays[r] = day
		for _, c := range columns {
			bounds := model.Object{"sheetId": prop.ID, "startRowIndex": r, "endRowIndex": r + 1, "startColumnIndex": c, "endColumnIndex": c + 1}
			if s.AutoLayout {
				requests = append(requests, model.Object{"repeatCell": model.Object{"range": bounds, "cell": model.Object{"userEnteredFormat": model.Object{"wrapStrategy": "WRAP"}}, "fields": "userEnteredFormat.wrapStrategy"}})
			}
			ranges = append(ranges, fmt.Sprintf("%s%s%d", a1(Names[0]), column(c+1), r+1))
		}
		if s.AutoLayout {
			requests = append(requests, model.Object{"autoResizeDimensions": model.Object{"dimensions": model.Object{"sheetId": prop.ID, "dimension": "ROWS", "startIndex": r, "endIndex": r + 1}}})
		}
	}
	if s.ActivityColors && len(ranges) > 0 {
		events, err := s.DB.AllEvents(ctx)
		if err != nil {
			return err
		}
		byDay := map[string][]model.Event{}
		for _, e := range events {
			d := day(e.Timestamp)
			byDay[d] = append(byDay[d], e)
		}
		for start := 0; start < len(ranges); start += 50 {
			var response struct {
				Sheets []struct {
					Data []struct {
						StartRow    int `json:"startRow"`
						StartColumn int `json:"startColumn"`
						Rows        []struct {
							Values []struct {
								Value  model.Object `json:"userEnteredValue"`
								Runs   []any        `json:"textFormatRuns"`
								Format struct {
									Text model.Object `json:"textFormat"`
								} `json:"userEnteredFormat"`
							} `json:"values"`
						} `json:"rowData"`
					} `json:"data"`
				} `json:"sheets"`
			}
			if err = s.call(ctx, "GET", "", url.Values{"ranges": ranges[start:min(start+50, len(ranges))], "fields": {"sheets(data(startRow,startColumn,rowData(values(userEnteredValue,textFormatRuns,userEnteredFormat(textFormat)))))"}}, nil, &response); err != nil {
				return err
			}
			for _, sh := range response.Sheets {
				for _, block := range sh.Data {
					for offset, row := range block.Rows {
						r := block.StartRow + offset
						for c, cell := range row.Values {
							text := cell.Value.Text("stringValue")
							if text == "" {
								continue
							}
							runs := colorRuns(text, byDay[rowDays[r]], cell.Runs, cell.Format.Text)
							if equal(runs, cell.Runs) {
								continue
							}
							requests = append(requests, model.Object{"updateCells": model.Object{"range": model.Object{"sheetId": prop.ID, "startRowIndex": r, "endRowIndex": r + 1, "startColumnIndex": block.StartColumn + c, "endColumnIndex": block.StartColumn + c + 1}, "rows": []any{model.Object{"values": []any{model.Object{"textFormatRuns": runs}}}}, "fields": "textFormatRuns"}})
						}
					}
				}
			}
		}
	}
	for start := 0; start < len(requests); start += 500 {
		var response model.Object
		if err := s.call(ctx, "POST", ":batchUpdate", nil, model.Object{"requests": requests[start:min(start+500, len(requests))]}, &response); err != nil {
			return err
		}
	}
	return nil
}
