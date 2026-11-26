package tables

import (
	"context"
	"daily-log/internal/model"
	"daily-log/internal/store"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Table struct {
	Rows [][]any
	Keys []any
}
type Change struct {
	Sheet       string
	Row, Column int
	Value       any
}
type plannedRow struct {
	Sheet, Target    string
	Number           int
	Before, After    []any
	Snapshot         []any
	PreserveSnapshot bool
}
type suggestion struct {
	Target string
	Values model.Object
}
type Plan struct {
	Changes     []Change
	Rows        []plannedRow
	Suggestions []suggestion
	Report      model.Object
}

func numericValue(v any) (float64, error) { return strconv.ParseFloat(fmt.Sprint(v), 64) }
func equal(a, b any) bool {
	x, _ := json.Marshal(blank(a))
	y, _ := json.Marshal(blank(b))
	if string(x) == string(y) {
		return true
	}
	if aa, ok := a.([]any); ok {
		bb, ok := b.([]any)
		if !ok || len(aa) != len(bb) {
			return false
		}
		for i := range aa {
			if !equal(aa[i], bb[i]) {
				return false
			}
		}
		return true
	}
	switch a.(type) {
	case map[string]any, model.Object:
		return false
	}
	if _, ok := a.(string); ok {
		if _, ok := b.(string); ok {
			return false
		}
	}
	na, ea := numericValue(a)
	nb, eb := numericValue(b)
	return ea == nil && eb == nil && !math.IsNaN(na) && na == nb
}
func padded(row []any, n int) []any {
	r := make([]any, n)
	for i := range r {
		r[i] = ""
		if i < len(row) {
			r[i] = blank(row[i])
		}
	}
	return r
}
func empty(row []any) bool {
	for _, v := range row {
		if !equal(v, "") {
			return false
		}
	}
	return true
}
func normalize(value any, daily bool) (string, error) {
	if !daily {
		return strings.TrimSpace(fmt.Sprint(blank(value))), nil
	}
	text := strings.TrimSpace(fmt.Sprint(blank(value)))
	if text == "" {
		return "", nil
	}
	if n, err := strconv.ParseFloat(text, 64); err == nil {
		return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(n)).Format(model.DateLayout), nil
	}
	t, err := model.ParseDay(strings.ReplaceAll(text, "/", "-"))
	if err != nil {
		return "", fmt.Errorf("无法识别已有日期；未写入")
	}
	return t.Format(model.DateLayout), nil
}
func Build(ctx context.Context, db *store.DB, base string, tables map[string]Table, data Dataset, privacy model.Redactor) (Plan, error) {
	p := Plan{Report: model.Object{}}
	// Validate every sheet before preparing any mutation.
	for _, name := range Names {
		t, ok := tables[name]
		if !ok || len(t.Rows) == 0 {
			return p, fmt.Errorf("%s 缺少工作表或表头；未写入", name)
		}
		headers := make([]string, len(t.Rows[0]))
		indexes := map[string]int{}
		for i, h := range t.Rows[0] {
			s := fmt.Sprint(h)
			if a := aliases[s]; a != "" {
				s = a
			}
			if _, ok := indexes[s]; ok {
				return p, fmt.Errorf("%s 存在重复或歧义表头", name)
			}
			headers[i] = s
			indexes[s] = i
		}
		for _, h := range Schemas[name] {
			if _, ok := indexes[h]; !ok {
				return p, fmt.Errorf("%s 缺少字段 %s", name, h)
			}
		}
		keyCol := indexes[Schemas[name][0]]
		keyed := map[string]int{}
		last := 0
		for i := 1; i < len(t.Rows); i++ {
			r := padded(t.Rows[i], len(headers))
			if empty(r) {
				continue
			}
			last = i
			v := r[keyCol]
			if i-1 < len(t.Keys) {
				v = t.Keys[i-1]
			}
			key, err := normalize(v, name == Names[0])
			if err != nil {
				return p, err
			}
			if key != "" {
				if _, ok := keyed[key]; ok {
					return p, fmt.Errorf("%s 已存在重复键；未写入", name)
				}
				keyed[key] = i
			}
		}
		report := model.Object{"written": 0, "protected": 0, "unchanged": 0, "skipped_private_keys": 0}
		p.Report[name] = map[string]any(report)
		inc := func(k string) { report[k] = report[k].(int) + 1 }
		keys := []string{}
		for k := range data[name] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if privacy.Text(key) != key {
				inc("skipped_private_keys")
				continue
			}
			values := privacy.Object(data[name][key])
			number, exists := keyed[key]
			before := padded(nil, len(headers))
			target := base + ":" + name + ":" + key
			if exists {
				before = padded(t.Rows[number], len(headers))
				snapshot, err := db.SheetSnapshot(ctx, target)
				if err != nil {
					return p, err
				}
				// Existing minute values are user supplied; keep them even on program-owned rows.
				if name == Names[0] {
					values = clone(values)
					for _, h := range Schemas[name][6:10] {
						values[h] = before[indexes[h]]
					}
					values[Schemas[name][0]] = before[keyCol]
					m := model.Object{}
					counts := map[string]int{}
					for i, c := range categories {
						m[c] = values[Schemas[name][6+i]]
						n, _ := numericValue(data["__counts"][key][c])
						counts[c] = int(n)
					}
					values["活动占比"] = privacy.Text(renderMix(counts, m))
				}
				matches := true
				for h, v := range values {
					if !equal(before[indexes[h]], v) {
						matches = false
						break
					}
				}
				if matches {
					inc("unchanged")
					continue
				}
				previous := snapshot
				comparison := append([]any(nil), before...)
				if name == Names[0] && snapshot != nil && len(snapshot) == len(before) {
					for _, h := range Schemas[name][5:10] {
						comparison[indexes[h]] = snapshot[indexes[h]]
					}
				}
				protected := snapshot == nil || !equal(snapshot, comparison)
				if name == Names[0] {
					status := fmt.Sprint(before[indexes["审核状态"]])
					manual := before[indexes["本人补充／纠正"]]
					if (status != "" && status != "待确认" && status != "待整理" && status != "已生成") || !equal(manual, "") {
						protected = true
					}
					template := status == "" || status == "待整理"
					for i, v := range before {
						h := headers[i]
						if h != "日期" && h != "星期" && h != "审核状态" && !equal(v, "") {
							template = false
						}
					}
					if snapshot == nil && template {
						protected = false
					}
				}
				for col, v := range before {
					if name == Names[0] {
						owned := false
						for _, h := range Schemas[name][5:10] {
							if col == indexes[h] {
								owned = true
							}
						}
						if owned {
							continue
						}
					}
					if s, ok := v.(string); ok && strings.HasPrefix(s, "=") {
						protected = true
					}
				}
				if protected {
					p.Suggestions = append(p.Suggestions, suggestion{target, values})
					inc("protected")
					if name == Names[0] {
						col := indexes["活动占比"]
						value := values["活动占比"]
						if !equal(before[col], value) {
							after := append([]any(nil), before...)
							after[col] = value
							p.Changes = append(p.Changes, Change{name, number, col, value})
							snap := append([]any(nil), previous...)
							if len(snap) == len(before) {
								snap[col] = value
							} else {
								snap = nil
							}
							p.Rows = append(p.Rows, plannedRow{Sheet: name, Target: target, Number: number, Before: before, After: after, Snapshot: snap, PreserveSnapshot: true})
						}
					}
					continue
				}
			} else {
				last++
				number = last
			}
			after := append([]any(nil), before...)
			columns := []int{}
			for h := range values {
				columns = append(columns, indexes[h])
			}
			sort.Ints(columns)
			for _, col := range columns {
				value := values[headers[col]]
				after[col] = value
				if !equal(before[col], value) {
					p.Changes = append(p.Changes, Change{name, number, col, value})
				}
			}
			p.Rows = append(p.Rows, plannedRow{Sheet: name, Target: target, Number: number, Before: before, After: after})
			inc("written")
		}
	}
	return p, nil
}
func clone(o model.Object) model.Object {
	n := model.Object{}
	for k, v := range o {
		n[k] = v
	}
	return n
}
func (p Plan) Verify(tables map[string]Table) error {
	for _, r := range p.Rows {
		t := tables[r.Sheet]
		if r.Number >= len(t.Rows) || !equal(padded(t.Rows[r.Number], len(r.After)), r.After) {
			return fmt.Errorf("%s 写入后核验不符；未提交同步快照", r.Sheet)
		}
	}
	return nil
}
func (p Plan) Commit(ctx context.Context, db *store.DB) error {
	return db.Transaction(ctx, func(tx *store.Tx) error {
		for _, r := range p.Rows {
			snapshot := r.After
			if r.PreserveSnapshot {
				snapshot = r.Snapshot
				if snapshot == nil {
					continue
				}
			}
			if err := tx.SaveSheetSnapshot(ctx, r.Target, snapshot); err != nil {
				return err
			}
		}
		for _, s := range p.Suggestions {
			if err := tx.Suggest(ctx, s.Target, "已确认／人工修改／现有内容无程序快照；保留原行", s.Values, time.Now()); err != nil {
				return err
			}
		}
		return nil
	})
}
