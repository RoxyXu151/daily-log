package tables

import (
	"bytes"
	"context"
	"crypto/sha256"
	"daily-log/internal/model"
	"daily-log/internal/privatefs"
	"daily-log/internal/store"
	"errors"
	"fmt"
	"github.com/xuri/excelize/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Excel struct {
	Path    string
	DB      *store.DB
	Privacy model.Redactor
}

func readExcel(f *excelize.File) (map[string]Table, error) {
	tables := map[string]Table{}
	for _, name := range f.GetSheetList() {
		rows, err := f.GetRows(name, excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, err
		}
		t := Table{}
		for r, rr := range rows {
			values := []any{}
			for c, v := range rr {
				cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
				formula, err := f.GetCellFormula(name, cell)
				if err != nil {
					return nil, err
				}
				if formula != "" {
					v = "=" + strings.TrimPrefix(formula, "=")
				}
				values = append(values, v)
			}
			t.Rows = append(t.Rows, values)
		}
		// Convert date-formatted Excel serials, including workbooks using the 1904 epoch.
		if name == Names[0] && len(t.Rows) > 0 {
			keyCol := 0
			for c, h := range t.Rows[0] {
				if h == "日期" {
					keyCol = c
				}
			}
			props, err := f.GetWorkbookProps()
			if err != nil {
				return nil, err
			}
			date1904 := props.Date1904 != nil && *props.Date1904
			for r := 1; r < len(t.Rows); r++ {
				v := any("")
				if keyCol < len(t.Rows[r]) {
					v = t.Rows[r][keyCol]
				}
				if date1904 {
					if n, err := numericValue(v); err == nil {
						v = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(n)).Format(model.DateLayout)
					}
				}
				t.Keys = append(t.Keys, v)
			}
		}
		tables[name] = t
	}
	return tables, nil
}
func (e Excel) Inspect(ctx context.Context) (model.Object, error) {
	if e.Path == "" {
		return nil, errors.New("请配置 excel.path")
	}
	result := model.Object{"path": e.Path, "exists": false, "runtime_ready": true}
	_, err := os.Stat(e.Path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := excelize.OpenFile(e.Path)
	if err != nil {
		return nil, errors.New("无法读取 Excel 工作簿")
	}
	defer f.Close()
	t, err := readExcel(f)
	if err != nil {
		return nil, err
	}
	rows := model.Object{}
	for n, t := range t {
		rows[n] = max(0, len(t.Rows)-1)
	}
	result["exists"], result["rows"] = true, rows
	return result, ctx.Err()
}
func (e Excel) Write(ctx context.Context) (model.Object, error) {
	if e.Path == "" {
		return nil, errors.New("请配置 excel.path")
	}
	if err := privatefs.Directory(filepath.Dir(e.Path)); err != nil {
		return nil, err
	}
	if err := privatefs.Restrict(e.Path); err != nil {
		return nil, err
	}
	lock := filepath.Join(filepath.Dir(e.Path), "~$"+filepath.Base(e.Path))
	if _, err := os.Stat(lock); err == nil {
		return nil, errors.New("Excel 文件正在被占用，请关闭后重试")
	}
	original, err := os.ReadFile(e.Path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var f *excelize.File
	if exists {
		f, err = excelize.OpenReader(bytes.NewReader(original))
		if err != nil {
			return nil, errors.New("无法读取 Excel 工作簿")
		}
	} else {
		f = excelize.NewFile()
		for _, n := range Names {
			if _, err = f.NewSheet(n); err != nil {
				return nil, err
			}
			for c, h := range Schemas[n] {
				cell, _ := excelize.CoordinatesToCellName(c+1, 1)
				if err = f.SetCellStr(n, cell, h); err != nil {
					return nil, err
				}
			}
		}
		if err = f.DeleteSheet("Sheet1"); err != nil {
			return nil, err
		}
	}
	defer f.Close()
	before, err := readExcel(f)
	if err != nil {
		return nil, err
	}
	data, err := Data(ctx, e.DB)
	if err != nil {
		return nil, err
	}
	plan, err := Build(ctx, e.DB, "https://sheets.googleapis.com/v4/spreadsheets/local-excel:"+e.Path, before, data, e.Privacy)
	if err != nil {
		return nil, err
	}
	for _, ch := range plan.Changes {
		cell, _ := excelize.CoordinatesToCellName(ch.Column+1, ch.Row+1)
		if err = f.SetCellValue(ch.Sheet, cell, ch.Value); err != nil {
			return nil, err
		}
	}
	if !exists || len(plan.Changes) > 0 {
		buffer, err := f.WriteToBuffer()
		if err != nil {
			return nil, err
		}
		check, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
		if err != nil {
			return nil, err
		}
		after, readErr := readExcel(check)
		check.Close()
		if readErr != nil {
			return nil, readErr
		}
		if err = plan.Verify(after); err != nil {
			return nil, err
		}
		allowed := map[string]bool{}
		for _, ch := range plan.Changes {
			allowed[fmt.Sprintf("%s:%d:%d", ch.Sheet, ch.Row, ch.Column)] = true
		}
		for name, t := range before {
			for r, row := range t.Rows {
				for c, v := range row {
					if allowed[fmt.Sprintf("%s:%d:%d", name, r, c)] {
						continue
					}
					a := after[name]
					if r >= len(a.Rows) || c >= len(a.Rows[r]) {
						if equal(v, "") {
							continue
						}
						return nil, errors.New("Excel 读回丢失原有单元格；保留原文件")
					}
					if !equal(v, a.Rows[r][c]) {
						return nil, errors.New("Excel 原有单元格读回不符；保留原文件")
					}
				}
			}
		}
		current, readErr := os.ReadFile(e.Path)
		if (exists && readErr != nil) || (!exists && !errors.Is(readErr, os.ErrNotExist)) || sha256.Sum256(current) != sha256.Sum256(original) {
			return nil, errors.New("写入期间 Excel 文件发生变化，请重试")
		}
		if _, err = os.Stat(lock); err == nil {
			return nil, errors.New("写入期间 Excel 文件被打开，请重试")
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if exists {
			dir := filepath.Join(filepath.Dir(e.Path), ".backups")
			if err = privatefs.Directory(dir); err != nil {
				return nil, err
			}
			stem := strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path))
			backup := filepath.Join(dir, stem+"-"+time.Now().Format("20060102-150405.000000000")+".xlsx")
			if err = privatefs.Write(backup, original); err != nil {
				return nil, err
			}
			old, _ := filepath.Glob(filepath.Join(dir, stem+"-*.xlsx"))
			sort.Strings(old)
			for len(old) > 5 {
				if err = os.Remove(old[0]); err != nil {
					return nil, err
				}
				old = old[1:]
			}
		}
		if err = privatefs.Write(e.Path, buffer.Bytes()); err != nil {
			return nil, err
		}
	}
	if err = plan.Commit(ctx, e.DB); err != nil {
		return nil, err
	}
	plan.Report["path"], plan.Report["verified_cells"] = e.Path, len(plan.Changes)
	return plan.Report, nil
}
