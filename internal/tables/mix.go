package tables

import (
	"daily-log/internal/model"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

var categories = []string{model.Research, model.Course, model.Career, model.Life}
var labels = []string{"科研", "课程", "职业", "生活"}

func allocate(weights []float64) []int {
	sum := 0.0
	for _, w := range weights {
		sum += w
	}
	r := make([]int, len(weights))
	if sum == 0 {
		return r
	}
	fractions := make([]float64, len(weights))
	order := []int{}
	left := 1000
	for i, w := range weights {
		x := w / sum * 1000
		r[i] = int(x)
		left -= r[i]
		fractions[i] = x - float64(r[i])
		order = append(order, i)
	}
	sort.SliceStable(order, func(i, j int) bool { return fractions[order[i]] > fractions[order[j]] })
	for _, i := range order[:left] {
		r[i]++
	}
	return r
}
func renderMix(counts map[string]int, minutes model.Object) string {
	weights := make([]float64, 4)
	total := 0.0
	for i, c := range categories {
		weights[i] = float64(counts[c])
		total += weights[i]
	}
	chart := func(w []float64, suffix string, missing map[int]bool) []string {
		sum := 0.0
		for _, v := range w {
			sum += v
		}
		shares := allocate(w)
		lines := []string{}
		for i, v := range w {
			if missing[i] {
				lines = append(lines, labels[i]+" 未记录")
				continue
			}
			bar := 0
			if sum > 0 {
				bar = int(v/sum*12 + 0.5)
			}
			lines = append(lines, fmt.Sprintf("%s %s%s %s%% · %g%s", labels[i], strings.Repeat("█", bar), strings.Repeat("░", 12-bar), strconv.FormatFloat(float64(shares[i])/10, 'f', -1, 64), v, suffix))
		}
		return lines
	}
	lines := []string{"数量占比：暂无事项记录"}
	if total > 0 {
		qualifier := ""
		if weights[1] > 0 {
			qualifier = "（含课程记录）"
		}
		lines = []string{fmt.Sprintf("数量占比%s · %g项", qualifier, total)}
		lines = append(lines, chart(weights, "项", nil)...)
	}
	known := 0
	sum := 0.0
	missing := map[int]bool{}
	for i, c := range categories {
		v, err := numericValue(minutes[c])
		_, boolean := minutes[c].(bool)
		if err != nil || boolean || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			missing[i] = true
			weights[i] = 0
			continue
		}
		known++
		weights[i] = v
		sum += v
	}
	if known == 0 {
		lines = append(lines, "用时占比：暂无时长记录", "实际分钟可在右侧四列填写")
	} else if sum == 0 {
		lines = append(lines, fmt.Sprintf("用时占比：已记录0分钟 · 已填%d/4类", known))
	} else {
		qualifier := ""
		if known < 4 {
			qualifier = fmt.Sprintf(" · 已填%d/4类", known)
		}
		lines = append(lines, fmt.Sprintf("用时占比 · %g分钟%s", sum, qualifier))
		lines = append(lines, chart(weights, "分钟", missing)...)
	}
	return strings.Join(lines, "\n")
}
