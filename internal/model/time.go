package model

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

const DateLayout = "2006-01-02"

var Shanghai = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return location
}()

func Day(t time.Time) time.Time {
	year, month, day := t.In(Shanghai).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, Shanghai)
}

func ParseDay(value string) (time.Time, error) {
	t, err := time.ParseInLocation(DateLayout, value, Shanghai)
	if err == nil && value != t.Format(DateLayout) {
		return time.Time{}, fmt.Errorf("invalid date %q", value)
	}
	return t, err
}

func ParseTime(value string, zone *time.Location) (time.Time, error) {
	// Legacy sources also use offset-free SQL timestamps and dates.
	value = normalizeSeparator(value)
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.In(Shanghai), nil
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05", value, zone)
	if err != nil && len(value) == len(DateLayout) {
		t, err = time.ParseInLocation(DateLayout, value, zone)
	}
	return t.In(Shanghai), err
}

func normalizeSeparator(value string) string {
	if len(value) > 10 && value[10] == ' ' {
		return value[:10] + "T" + value[11:]
	}
	return value
}

// ISO retains the six-digit fractional form used by existing IDs and watermarks.
func ISO(t time.Time) string {
	return formatTime(t.In(Shanghai), "-07:00")
}

func UTC(t time.Time) string {
	return formatTime(t.UTC(), "Z")
}

func formatTime(t time.Time, suffix string) string {
	layout := "2006-01-02T15:04:05"
	if t.Nanosecond()/1000 != 0 {
		layout += ".000000"
	}
	return t.Format(layout + suffix)
}

const DefaultDailyClock = "23:00"

func ValidDailyClock(clock string) bool {
	parsed, err := time.Parse("15:04", clock)
	return err == nil && parsed.Format("15:04") == clock
}

func DailyRunTime(at time.Time, clock string) time.Time {
	if !ValidDailyClock(clock) {
		clock = DefaultDailyClock
	}
	parsed, _ := time.Parse("15:04", clock)
	return Day(at).Add(time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute)
}

func NextDailyRun(at time.Time, clock string) time.Time {
	next := DailyRunTime(at, clock)
	if !at.Before(next) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
