package tables

import (
	"context"
	"daily-log/internal/model"
	"daily-log/internal/netclient"
	"daily-log/internal/store"
	"encoding/json"
	"github.com/xuri/excelize/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func seed(t *testing.T, db *store.DB, text string) {
	t.Helper()
	err := db.Transaction(context.Background(), func(tx *store.Tx) error {
		return tx.SaveJournal(context.Background(), model.Record{Day: "2026-11-12", Status: "已生成", Payload: model.Object{"上午": text, "下午": "", "晚上": "", "manual": "", "status": "已生成", "minutes": map[string]any{}, "todoist_count": 1, "zotero_count": 0, "evidence": []any{}, "updated_at": "2026-11-12T12:00:00+08:00"}})
	})
	if err != nil {
		t.Fatal(err)
	}
}
func emptyTables() map[string]Table {
	m := map[string]Table{}
	for _, n := range Names {
		h := []any{}
		for _, v := range Schemas[n] {
			h = append(h, v)
		}
		m[n] = Table{Rows: [][]any{h}}
	}
	return m
}
func TestPlannerProtectionAndPrivacy(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	seed(t, db, "secret-value 的讨论")
	data, err := Data(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	tables := emptyTables()
	privacy := model.NewRedactor([]string{"secret-value"})
	p, err := Build(ctx, db, "test", tables, data, privacy)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range p.Rows {
		tt := tables[r.Sheet]
		for len(tt.Rows) <= r.Number {
			tt.Rows = append(tt.Rows, nil)
		}
		tt.Rows[r.Number] = r.After
		tables[r.Sheet] = tt
	}
	if err = p.Commit(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, ch := range p.Changes {
		if strings.Contains(stringValue(ch.Value), "secret-value") {
			t.Fatal("credential leaked")
		}
	}
	again, err := Build(ctx, db, "test", tables, data, privacy)
	if err != nil || len(again.Changes) != 0 {
		t.Fatalf("not idempotent: %v %d", err, len(again.Changes))
	}
	tables[Names[0]].Rows[1][2] = "人工保留"
	seed(t, db, "新的事实")
	data, _ = Data(ctx, db)
	p, err = Build(ctx, db, "test", tables, data, privacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Suggestions) != 1 {
		t.Fatalf("missing protection: %+v", p.Report)
	}
	for _, ch := range p.Changes {
		if ch.Sheet == Names[0] {
			t.Fatal("manual row overwritten")
		}
	}
	tt := tables[Names[1]]
	tt.Rows = append(tt.Rows, []any{"duplicate"}, []any{"duplicate"})
	tables[Names[1]] = tt
	if _, err = Build(ctx, db, "test", tables, data, privacy); err == nil {
		t.Fatal("duplicate accepted")
	}
}
func stringValue(v any) string { s, _ := v.(string); return s }
func TestExcelNativePreservesManualAndFormulas(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	seed(t, db, "=来源文本")
	path := filepath.Join(t.TempDir(), "journal.xlsx")
	e := Excel{Path: path, DB: db}
	if _, err := e.Write(ctx); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if formula, _ := f.GetCellFormula(Names[0], "C2"); formula != "" {
		t.Fatal("source interpreted as formula")
	}
	if v, _ := f.GetCellValue(Names[0], "C2"); v != "=来源文本" {
		t.Fatalf("text = %q", v)
	}
	f.SetCellStr(Names[0], "C2", "人工正文")
	f.NewSheet("私人页")
	f.SetCellFormula("私人页", "A1", "1+2")
	f.SetCellStr("私人页", "B1", "保留")
	style, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FF0000"}})
	f.SetCellStyle("私人页", "B1", "B1", style)
	if err = f.Save(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	seed(t, db, "更新正文")
	report, err := e.Write(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report[Names[0]].(map[string]any)["protected"] != 1 {
		t.Fatalf("manual not protected: %+v", report)
	}
	f, err = excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if v, _ := f.GetCellValue(Names[0], "C2"); v != "人工正文" {
		t.Fatal("manual overwritten")
	}
	if v, _ := f.GetCellFormula("私人页", "A1"); v != "1+2" {
		t.Fatal("formula lost")
	}
	if v, _ := f.GetCellStyle("私人页", "B1"); v != style {
		t.Fatal("style lost")
	}
	lock := filepath.Join(filepath.Dir(path), "~$journal.xlsx")
	os.WriteFile(lock, nil, 0600)
	if _, err = e.Write(ctx); err == nil {
		t.Fatal("open workbook overwritten")
	}
}
func TestSheetsReadbackAndRawWrites(t *testing.T) {
	for _, badReadback := range []bool{false, true} {
		t.Run(map[bool]string{false: "verified", true: "mismatch"}[badReadback], func(t *testing.T) {
			ctx := context.Background()
			db := testDB(t)
			seed(t, db, "=SUM(1,2)")
			tables := emptyTables()
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/values:batchGet") {
					ranges := []valueRange{}
					for _, rr := range r.URL.Query()["ranges"] {
						parts := strings.Split(rr, "!")
						name := strings.Trim(parts[0], "'")
						bounds := strings.Split(parts[1], ":")
						start := 0
						for _, c := range bounds[0] {
							if c >= '0' && c <= '9' {
								start = start*10 + int(c-'0')
							}
						}
						end := start
						if len(bounds) > 1 {
							end = 0
							for _, c := range bounds[1] {
								if c >= '0' && c <= '9' {
									end = end*10 + int(c-'0')
								}
							}
						}
						rows := [][]any{}
						for i := start - 1; i < min(end, len(tables[name].Rows)); i++ {
							rows = append(rows, tables[name].Rows[i])
						}
						if badReadback && writes > 0 && name == Names[0] {
							rows = nil
						}
						ranges = append(ranges, valueRange{Range: rr, Values: rows})
					}
					json.NewEncoder(w).Encode(map[string]any{"valueRanges": ranges})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/values:batchUpdate") {
					var body struct {
						Mode string       `json:"valueInputOption"`
						Data []valueRange `json:"data"`
					}
					json.NewDecoder(r.Body).Decode(&body)
					if body.Mode != "RAW" {
						t.Error("unsafe input mode")
					}
					writes++
					for _, entry := range body.Data {
						parts := strings.Split(entry.Range, "!")
						name := strings.Trim(parts[0], "'")
						c, row := 0, 0
						for _, v := range parts[1] {
							if v >= 'A' && v <= 'Z' {
								c = c*26 + int(v-'A') + 1
							} else {
								row = row*10 + int(v-'0')
							}
						}
						tt := tables[name]
						for len(tt.Rows) < row {
							tt.Rows = append(tt.Rows, nil)
						}
						tt.Rows[row-1] = padded(tt.Rows[row-1], max(c, len(tt.Rows[row-1])))
						tt.Rows[row-1][c-1] = entry.Values[0][0]
						tables[name] = tt
					}
					w.Write([]byte(`{}`))
					return
				}
				m := metadata{ID: "test"}
				for i, n := range Names {
					p := sheetProperty{Title: n, ID: i}
					p.Grid.Rows = 500
					p.Grid.Columns = len(Schemas[n])
					m.Sheets = append(m.Sheets, struct {
						Properties sheetProperty `json:"properties"`
					}{p})
				}
				json.NewEncoder(w).Encode(m)
			}))
			defer server.Close()
			s := Sheets{ID: "test", DB: db, BaseURL: server.URL, HTTP: netclient.Client{HTTP: &http.Client{Timeout: time.Second}}}
			_, err := s.Write(ctx)
			if badReadback {
				if err == nil {
					t.Fatal("invalid readback committed")
				}
				snapshot, _ := db.SheetSnapshot(ctx, s.base()+":"+Names[0]+":2026-11-12")
				if snapshot != nil {
					t.Fatal("snapshot advanced on failure")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if writes == 0 {
					t.Fatal("nothing written")
				}
				if _, err = s.Write(ctx); err != nil {
					t.Fatal(err)
				}
				if writes != 1 {
					t.Fatal("unchanged sync wrote again")
				}
			}
		})
	}
}

func TestMixInputDoesNotBlessOtherManualEdits(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	seed(t, db, "原始正文")
	data, _ := Data(ctx, db)
	tables := emptyTables()
	p, err := Build(ctx, db, "test", tables, data, model.Redactor{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range p.Rows {
		tt := tables[r.Sheet]
		for len(tt.Rows) <= r.Number {
			tt.Rows = append(tt.Rows, nil)
		}
		tt.Rows[r.Number] = r.After
		tables[r.Sheet] = tt
	}
	if err = p.Commit(ctx, db); err != nil {
		t.Fatal(err)
	}
	// A minute edit alone must allow fresh program text while preserving that input.
	tables[Names[0]].Rows[1][6] = 30
	seed(t, db, "更新正文")
	data, _ = Data(ctx, db)
	p, err = Build(ctx, db, "test", tables, data, model.Redactor{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range p.Rows {
		if r.Sheet == Names[0] {
			if r.After[2] != "更新正文" || r.After[6] != 30 || !strings.Contains(stringValue(r.After[5]), "30分钟") {
				t.Fatalf("minute input lost or blocked update: %+v", r.After)
			}
		}
	}
	// A protected row may refresh only its derived mix column and must retain old text snapshot.
	tables[Names[0]].Rows[1][2] = "人工正文"
	p, err = Build(ctx, db, "test", tables, data, model.Redactor{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Changes {
		if c.Sheet == Names[0] && c.Column != 5 {
			t.Fatal("manual content overwritten")
		}
	}
	if err = p.Commit(ctx, db); err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.SheetSnapshot(ctx, "test:"+Names[0]+":2026-11-12")
	if err != nil || snapshot[2] != "原始正文" {
		t.Fatal("manual text blessed as program output")
	}
}
func TestMixRoundingAndColorRuns(t *testing.T) {
	text := renderMix(map[string]int{model.Research: 1, model.Course: 1, model.Career: 1}, model.Object{})
	if !strings.Contains(text, "33.4%") || strings.Count(text, "33.3%") != 2 || !strings.Contains(text, "暂无时长记录") {
		t.Fatal(text)
	}
	old := []any{model.Object{"startIndex": 0, "format": model.Object{"bold": true, "link": model.Object{"uri": "https://example.com"}}}}
	runs := colorRuns("科研 😀\n课程 活动", nil, old, nil)
	if len(runs) != 2 {
		t.Fatal(runs)
	}
	first := asObject(runs[0])
	format := asObject(first["format"])
	if format["bold"] != true || asObject(format["link"]).Text("uri") == "" {
		t.Fatal("text formatting lost")
	}
	index, _ := numericValue(asObject(runs[1])["startIndex"])
	if index != 6 {
		t.Fatalf("bad UTF16 offset %v", index)
	}
}
