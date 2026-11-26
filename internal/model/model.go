package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	Research      = "科研学习与论文"
	Course        = "课程学习"
	Career        = "职业学习与求职"
	Life          = "校园事务与个人生活"
	Uncategorized = "未分类"
)

type Object map[string]any

func (o Object) Text(key string) string {
	v, _ := o[key].(string)
	return v
}

func (o Object) StringOr(key, fallback string) string {
	if v, ok := o[key].(string); ok {
		return v
	}
	return fallback
}

func (o Object) Bool(key string) bool {
	v, _ := o[key].(bool)
	return v
}

func (o Object) Object(key string) Object {
	v, _ := o[key].(map[string]any)
	return v
}

func (o Object) List(key string) []any {
	v, _ := o[key].([]any)
	return v
}

type Event struct {
	ID          string  `json:"event_id"`
	Source      string  `json:"source"`
	ObjectID    string  `json:"source_object_id"`
	Kind        string  `json:"event_type"`
	Title       string  `json:"title"`
	Timestamp   string  `json:"timestamp"`
	Description string  `json:"description"`
	Start       *string `json:"start_time"`
	End         *string `json:"end_time"`
	Category    string  `json:"category"`
	Evidence    string  `json:"evidence_type"`
	Confidence  Float   `json:"confidence"`
	URL         string  `json:"source_url"`
	Updated     string  `json:"last_updated"`
	Raw         Object  `json:"raw"`
	Metadata    Object  `json:"metadata"`
}

func (e *Event) UnmarshalJSON(data []byte) error {
	type fields Event
	*e = Event{Category: Life, Evidence: "direct", Confidence: 1, Raw: Object{}, Metadata: Object{}}
	return Decode(data, (*fields)(e))
}

func NewEvent(id, source, objectID, kind, title string, at time.Time) Event {
	return Event{
		ID: id, Source: source, ObjectID: objectID, Kind: kind, Title: title,
		Timestamp: ISO(at), Category: Life, Evidence: "direct", Confidence: 1,
		Raw: Object{}, Metadata: Object{},
	}
}

type Session struct {
	ID       string   `json:"session_id"`
	PaperID  string   `json:"zotero_item_id"`
	Title    string   `json:"paper_title"`
	First    string   `json:"first_annotation_time"`
	Last     string   `json:"last_annotation_time"`
	Count    int      `json:"annotation_count"`
	Pages    []string `json:"pages"`
	Span     *Float   `json:"estimated_activity_window"`
	Duration *Float   `json:"measured_active_duration"`
	Method   string   `json:"inference_method"`
	Summary  string   `json:"summary"`
	URL      string   `json:"source_url"`
	Active   bool     `json:"active"`
}

func (s *Session) UnmarshalJSON(data []byte) error {
	type fields Session
	*s = Session{Active: true}
	return json.Unmarshal(data, (*fields)(s))
}

type Record struct {
	Day       string `json:"day"`
	Payload   Object `json:"payload"`
	InputHash string `json:"input_hash"`
	Status    string `json:"status"`
}

type Float float64

func (f Float) MarshalJSON() ([]byte, error) {
	s := strconv.FormatFloat(float64(f), 'g', -1, 64)
	// Python prints finite integral floats in fixed notation up to 1e16.
	if magnitude := math.Abs(float64(f)); magnitude == 0 || magnitude >= 1e-4 && magnitude < 1e16 {
		s = strconv.FormatFloat(float64(f), 'f', -1, 64)
	}
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	return []byte(s), nil
}

func Decode(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("JSON contains trailing data")
	}
	return nil
}

// CanonicalJSON matches the original Python JSON layout used by persistent IDs,
// fingerprints and suggestion uniqueness constraints.
func CanonicalJSON(value any) (string, error) {
	encoded, err := encode(value, "")
	if err != nil {
		return "", err
	}
	var generic any
	if err = Decode(encoded, &generic); err != nil {
		return "", err
	}
	encoded, err = encode(canonicalNumbers(generic), "")
	if err != nil {
		return "", err
	}
	var out strings.Builder
	inString := false
	for i := 0; i < len(encoded); i++ {
		c := encoded[i]
		switch {
		case inString && c == '\\':
			if bytes.HasPrefix(encoded[i:], []byte(`\u2028`)) {
				out.WriteRune('\u2028')
				i += 5
			} else if bytes.HasPrefix(encoded[i:], []byte(`\u2029`)) {
				out.WriteRune('\u2029')
				i += 5
			} else {
				out.WriteByte(c)
				i++
				out.WriteByte(encoded[i])
			}
		case c == '"':
			inString = !inString
			out.WriteByte(c)
		default:
			out.WriteByte(c)
			if !inString && (c == ',' || c == ':') {
				out.WriteByte(' ')
			}
		}
	}
	return out.String(), nil
}

func canonicalNumbers(value any) any {
	switch value := value.(type) {
	case json.Number:
		if strings.ContainsAny(value.String(), ".eE") {
			if number, err := value.Float64(); err == nil {
				return Float(number)
			}
		}
	case map[string]any:
		for key, entry := range value {
			value[key] = canonicalNumbers(entry)
		}
	case []any:
		for i, entry := range value {
			value[i] = canonicalNumbers(entry)
		}
	}
	return value
}

func encode(value any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", indent)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func PrettyJSON(value any) ([]byte, error) {
	data, err := encode(value, "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func Fingerprint(value any) (string, error) {
	encoded, err := CanonicalJSON(value)
	if err != nil {
		return "", fmt.Errorf("encode fingerprint: %w", err)
	}
	return FingerprintJSON(encoded), nil
}

func FingerprintJSON(encoded string) string {
	sum := sha256.Sum256([]byte(encoded))
	return hex.EncodeToString(sum[:])
}

func StableID(parts ...string) string {
	hash, err := Fingerprint(parts)
	if err != nil {
		panic(err) // A slice of strings is always JSON encodable.
	}
	return hash[:32]
}
