package codex

import (
	"errors"
	"regexp"
	"strings"

	"daily-log/internal/model"
)

type replacement struct {
	pattern *regexp.Regexp
	value   string
}

var redactions = []replacement{
	{regexp.MustCompile(`(?s)<environment_context\b[^>]*>.*?</environment_context>`), ""},
	{regexp.MustCompile(`(?s)<in-app-browser-context\b[^>]*>.*?</in-app-browser-context>`), ""},
	{regexp.MustCompile(`(?s)<system-reminder\b[^>]*>.*?</system-reminder>`), ""},
	{regexp.MustCompile(`:codex-[\w-]+\{[^}]*\}`), ""},
	{regexp.MustCompile("(?s)\x60\x60\x60[^\n]*\n.*?(?:\x60\x60\x60|$)"), "[代码块]"},
	{regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`), "[已隐藏凭证]"},
	{regexp.MustCompile("\\*\\*|\x60"), ""},
	{regexp.MustCompile(`[\pL\pN_.+-]+@[\pL\pN_.-]+\.[A-Za-z]{2,}`), "[邮箱]"},
	{regexp.MustCompile(`(?:\d{1,3}\.){3}\d{1,3}`), "[地址]"},
	{regexp.MustCompile(`(?:/(?:home|Users|mnt|tmp|data)/|[A-Za-z]:\\)\S+`), "[本地路径]"},
	{regexp.MustCompile(`(?i)\bBearer\s+\S+`), "Bearer [已隐藏凭证]"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]+`), "[已隐藏凭证]"},
	{regexp.MustCompile(`\b[A-Za-z0-9_-]{32,}\b`), "[已隐藏标识]"},
	{regexp.MustCompile(`(?im)((?:api[_ -]?key|token|password|secret|密码|密钥)\s*[:=：]\s*)(?:"[^"\n]*"|'[^'\n]*'|\S+)`), "${1}[已隐藏凭证]"},
	{regexp.MustCompile(`https?://\S+`), "[链接]"},
}

func clean(text string) string {
	for _, rule := range redactions[:3] {
		text = rule.pattern.ReplaceAllString(text, rule.value)
	}
	if i := strings.LastIndex(text, "## My request:"); i >= 0 {
		text = text[i+len("## My request:"):]
	}
	for _, rule := range redactions[3:] {
		text = rule.pattern.ReplaceAllString(text, rule.value)
	}
	return strings.TrimSpace(text)
}

var (
	completion = regexp.MustCompile(`已完成|已提交|已修复|已实现|成功交付|耗时`)
	short      = regexp.MustCompile(`(?i)^(继续|直接记录|开始|执行|修复|可以|好|是的|yes|ok|go)[。！!. ]*$`)
	idle       = regexp.MustCompile(`(?i)^(你好|谢谢|多谢|早上好|晚安|hi|hello|thanks)[。！!. ]*$`)
)

type categoryPattern struct {
	category string
	pattern  *regexp.Regexp
}

type policy struct {
	rules     []categoryPattern
	overrides map[string]string
}

var categories = []categoryPattern{
	{model.Course, regexp.MustCompile(`(?i)课程|课表|课堂|作业|考试|学分|coursework|homework`)},
	{model.Career, regexp.MustCompile(`(?i)求职|面试|简历|招聘|实习申请|职业技能|interview|résumé`)},
	{model.Research, regexp.MustCompile(`(?i)论文|科研|文献|课题|消融|实验|同行评审|manuscript|research`)},
}

func compilePolicy(rules []CategoryRule, overrides map[string]string) (policy, error) {
	valid := func(category string) bool {
		switch category {
		case model.Course, model.Career, model.Research, model.Life:
			return true
		default:
			return false
		}
	}
	p := policy{overrides: overrides}
	for _, category := range overrides {
		if category != "" && !valid(category) {
			return p, errors.New("Codex 会话分类覆盖无效")
		}
	}
	for _, rule := range rules {
		pattern, err := regexp.Compile("(?i)" + rule.Pattern)
		if err != nil || !valid(rule.Category) {
			return p, errors.New("Codex 分类规则无效，请使用 Go 正则表达式及已支持的类别")
		}
		p.rules = append(p.rules, categoryPattern{rule.Category, pattern})
	}
	return p, nil
}

func (p policy) classify(thread, text, title string) (string, string) {
	if category := p.overrides[thread]; category != "" {
		return category, "thread_override"
	}
	for _, rule := range p.rules {
		if rule.pattern.MatchString(text + " " + title) {
			return rule.category, "custom_rule"
		}
	}
	for _, candidate := range []string{text, title} {
		for _, rule := range categories {
			if rule.pattern.MatchString(candidate) {
				return rule.category, "evidence_keywords"
			}
		}
	}
	return model.Life, "default"
}

func clip(text string, limit int) string {
	runes := []rune(text)
	return string(runes[:min(len(runes), limit)])
}

func excerpt(text string, limit int) string {
	return clip(strings.Join(strings.Fields(text), " "), limit)
}

func contained(quote, text string) bool {
	quote = strings.Join(strings.Fields(quote), "")
	return quote != "" && strings.Contains(strings.Join(strings.Fields(text), ""), quote)
}
