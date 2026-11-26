package model

import (
	"net/url"
	"slices"
	"strings"
)

// Redactor masks credentials at presentation and export boundaries.
type Redactor struct {
	replace  *strings.Replacer
	patterns []string
}

func NewRedactor(values []string) Redactor {
	seen := map[string]bool{}
	var patterns []string
	for _, value := range values {
		if value == "" {
			continue
		}
		for _, pattern := range []string{value, url.QueryEscape(value), url.PathEscape(value)} {
			if !seen[pattern] {
				seen[pattern] = true
				patterns = append(patterns, pattern)
			}
		}
	}
	if len(patterns) == 0 {
		return Redactor{}
	}
	slices.SortFunc(patterns, func(a, b string) int { return len(b) - len(a) })
	pairs := make([]string, 0, 2*len(patterns))
	for _, pattern := range patterns {
		pairs = append(pairs, pattern, "[已隐藏凭证]")
	}
	return Redactor{replace: strings.NewReplacer(pairs...), patterns: patterns}
}

// Patterns returns a copy of the literal credential match rules.
func (r Redactor) Patterns() []string { return slices.Clone(r.patterns) }

func (r Redactor) Text(text string) string {
	if r.replace == nil {
		return text
	}
	return r.replace.Replace(text)
}

func (r Redactor) Err(err error) error {
	if err == nil {
		return nil
	}
	message := r.Text(err.Error())
	if message == err.Error() {
		return err
	}
	return maskedError{message: message, cause: err}
}

type maskedError struct {
	message string
	cause   error
}

func (e maskedError) Error() string { return e.message }
func (e maskedError) Unwrap() error { return e.cause }

// Object returns a copy with JSON text values masked.
func (r Redactor) Object(object Object) Object {
	if object == nil || r.replace == nil {
		return object
	}
	result := make(Object, len(object))
	for key, value := range object {
		result[key] = r.value(value)
	}
	return result
}

func (r Redactor) value(value any) any {
	switch value := value.(type) {
	case string:
		return r.Text(value)
	case Object:
		return r.Object(value)
	case map[string]any:
		return map[string]any(r.Object(Object(value)))
	case []Object:
		result := make([]Object, len(value))
		for i, item := range value {
			result[i] = r.Object(item)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = r.value(item)
		}
		return result
	default:
		return value
	}
}
