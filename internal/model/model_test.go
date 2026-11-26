package model

import (
	"testing"
	"time"
)

func TestLegacyCanonicalJSON(t *testing.T) {
	var input Object
	if err := Decode([]byte(`{"z":1.00,"title":"中文<&\u2028\n","id":9007199254740993,"a":1E-6}`), &input); err != nil {
		t.Fatal(err)
	}
	got, err := CanonicalJSON(input)
	want := "{\"a\": 1e-06, \"id\": 9007199254740993, \"title\": \"中文<&\u2028\\n\", \"z\": 1.0}"
	if err != nil || got != want {
		t.Fatalf("CanonicalJSON = %q, %v; want %q", got, err, want)
	}
	instant, err := ParseTime("2026-11-08T16:01:00.013Z", time.UTC)
	if err != nil || ISO(instant) != "2026-11-09T00:01:00.013000+08:00" {
		t.Fatalf("unexpected timestamp: %s, %v", ISO(instant), err)
	}
}
