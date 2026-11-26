// Package tables synchronizes managed cells without overwriting manual edits.
package tables

import (
	"context"
	"daily-log/internal/journal"
	"daily-log/internal/model"
	"daily-log/internal/store"
	"fmt"
	"sort"
	"strings"
	"time"
)

var Names = []string{"每日活动", "活动流水", "论文阅读", "统计看板"}
var Schemas = map[string][]string{
	"每日活动": {"日期", "星期", "上午（06-12）", "下午（12-18）", "晚上（18-24）", "活动占比", "科研学习（分钟）", "课程学习（分钟）", "职业学习（分钟）", "校园与生活（分钟）", "已完成任务数", "论文标注数", "审核状态", "本人补充／纠正", "证据来源", "最后整理时间"},
	"活动流水": {"事件ID", "来源", "事件类型", "记录日期（北京时间）", "开始时间", "结束时间", "标题／活动", "内容摘要", "类别", "关联对象ID", "证据链接", "证据性质", "可信级别", "是否写入日记", "事件更新时间"},
	"论文阅读": {"会话ID", "Zotero 论文ID", "论文标题", "开始时间", "结束时间", "标注数量", "标注跨度（分钟）", "活跃阅读时长（分钟）", "页码范围", "阅读／笔记摘要", "推断依据", "精度", "链接", "更新时间"},
	"统计看板": {"指标", "数值", "说明", "更新时间"},
}
var aliases = map[string]string{"今日成果": "活动占比", "上午": "上午（06-12）", "下午": "下午（12-18）", "晚上": "晚上（18-24）", "手动补充": "本人补充／纠正", "Todoist 完成任务数": "已完成任务数", "Zotero 标注数": "论文标注数", "最后更新时间": "最后整理时间"}

type Dataset map[string]map[string]model.Object

func blank(v any) any {
	if v == nil {
		return ""
	}
	return v
}
func pointer(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func minutes(v *model.Float) any {
	if v == nil {
		return ""
	}
	return *v
}
func day(stamp string) string {
	t, err := model.ParseTime(stamp, time.UTC)
	if err != nil {
		return ""
	}
	return model.Day(t).Format(model.DateLayout)
}
func row(name string, values ...any) model.Object {
	r := model.Object{}
	for i, h := range Schemas[name] {
		r[h] = blank(values[i])
	}
	return r
}

func Data(ctx context.Context, db *store.DB) (Dataset, error) {
	result := Dataset{"__counts": map[string]model.Object{}}
	for _, n := range Names {
		result[n] = map[string]model.Object{}
	}
	journals, err := db.Journals(ctx)
	if err != nil {
		return nil, err
	}
	events, err := db.AllEvents(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := db.AllSessions(ctx)
	if err != nil {
		return nil, err
	}
	cited := map[string]bool{}
	updated := ""
	counts := map[string]map[string]int{}
	byDay := map[string][]model.Event{}
	for _, e := range events {
		if e.Metadata.Bool("cancelled") {
			continue
		}
		d := day(e.Timestamp)
		if counts[d] == nil {
			counts[d] = map[string]int{}
		}
		byDay[d] = append(byDay[d], e)
		updated = max(updated, e.Updated, e.Timestamp)
	}
	for d, events := range byDay {
		counts[d] = journal.ActivityCounts(events)
		result["__counts"][d] = model.Object{}
		for c, n := range counts[d] {
			result["__counts"][d][c] = n
		}
	}
	for _, j := range journals {
		p := j.Payload
		t, err := model.ParseDay(j.Day)
		if err != nil {
			return nil, err
		}
		evidence, _ := model.CanonicalJSON(p["evidence"])
		for _, v := range p.List("evidence") {
			if e, ok := v.(map[string]any); ok {
				cited[model.Object(e).Text("event_id")] = true
			}
		}
		if len([]rune(string(evidence))) > 12000 {
			evidence = fmt.Sprintf("完整证据见 SQLite 日记 %s 与活动流水", j.Day)
		}
		m := p.Object("minutes")
		morning := p.Text("上午")
		var missing []string
		for k, v := range p.Object("coverage") {
			if v != "ok" {
				missing = append(missing, k)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			morning = "采集覆盖不完整：" + strings.Join(missing, "、") + "。\n" + morning
		}
		result[Names[0]][j.Day] = row(Names[0], j.Day, "周"+[]string{"日", "一", "二", "三", "四", "五", "六"}[t.Weekday()], morning, p.Text("下午"), p.Text("晚上"), renderMix(counts[j.Day], m), m[model.Research], m[model.Course], m[model.Career], m[model.Life], p["todoist_count"], p["zotero_count"], p["status"], p["manual"], string(evidence), p["updated_at"])
		updated = max(updated, p.Text("updated_at"))
	}
	for _, e := range events {
		written := "否"
		if cited[e.ID] {
			written = "是"
		}
		result[Names[1]][e.ID] = row(Names[1], e.ID, e.Source, e.Kind, day(e.Timestamp), first(pointer(e.Start), e.Timestamp), pointer(e.End), e.Title, e.Description, e.Category, e.ObjectID, e.URL, e.Evidence, e.Confidence, written, e.Updated)
	}
	for _, s := range sessions {
		precision := "候选会话；实际阅读时长未知"
		if !s.Active {
			precision = "已被重新聚合替代（历史记录）"
		}
		result[Names[2]][s.ID] = row(Names[2], s.ID, s.PaperID, s.Title, s.First, s.Last, s.Count, minutes(s.Span), minutes(s.Duration), strings.Join(s.Pages, "、"), s.Summary, s.Method, precision, s.URL, s.Last)
	}
	today := model.Day(time.Now())
	week := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
	for _, period := range []struct {
		name  string
		start string
	}{{"今日", today.Format(model.DateLayout)}, {"本周", week.Format(model.DateLayout)}, {"本月", today.Format("2006-01") + "-01"}, {"全部", ""}} {
		metrics := map[string]int{"本人标记完成任务": 0, "课程勾选记录": 0, "独立日程安排": 0, "论文新建标注": 0, "候选阅读会话": 0}
		within := func(d string) bool { return d != "" && d >= period.start && d <= today.Format(model.DateLayout) }
		for _, e := range events {
			if !within(day(e.Timestamp)) || e.Metadata.Bool("cancelled") {
				continue
			}
			if e.Source == "todoist" && e.Kind == "task_completed" {
				metrics["本人标记完成任务"]++
			}
			if e.Kind == "course_checked" {
				metrics["课程勾选记录"]++
			}
			if e.Source == "calendar" {
				metrics["独立日程安排"]++
			}
			if e.Source == "zotero" && e.Kind == "annotation_created" {
				metrics["论文新建标注"]++
			}
		}
		for _, s := range sessions {
			if s.Active && within(day(s.First)) {
				metrics["候选阅读会话"]++
			}
		}
		for metric, count := range metrics {
			k := period.name + "｜" + metric
			note := "按北京时间和事件时间归档；本周从星期一开始"
			if metric == "候选阅读会话" {
				note = "基于标注间隔推断，不代表测得活跃阅读时长"
			}
			result[Names[3]][k] = row(Names[3], k, count, note, updated)
		}
	}
	for _, status := range []string{"待确认", "已确认"} {
		count := 0
		for _, j := range journals {
			if j.Status == status {
				count++
			}
		}
		k := "本地日志｜" + status
		result[Names[3]][k] = row(Names[3], k, count, "本地状态；表格中人工确认的行受保护", updated)
	}
	k := "时长｜测得活跃分钟"
	result[Names[3]][k] = row(Names[3], k, "未知", "当前来源未提供活跃时长测量", updated)
	return result, nil
}
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
