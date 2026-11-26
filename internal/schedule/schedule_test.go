package schedule

import (
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"daily-log/internal/model"
)

func TestTaskDefinitionsUseShanghaiTimeAndBackgroundEntry(t *testing.T) {
	job := Job{Binary: `C:\Daily Log & tools\daily-log.exe`, Config: `C:\个人 日记\config.yaml`, Clock: "21:35"}
	when := time.Date(2026, 11, 10, 21, 35, 0, 0, model.Shanghai)
	raw := windowsTask(job, "S-1-5-21-10", "run", when)
	var task struct {
		Command   string `xml:"Actions>Exec>Command"`
		Arguments string `xml:"Actions>Exec>Arguments"`
		Boundary  string `xml:"Triggers>CalendarTrigger>StartBoundary"`
		User      string `xml:"Principals>Principal>UserId"`
	}
	// Encoding is converted to UTF-16 when the XML is written. Here the
	// in-memory string is UTF-8, so decode just the document element.
	if err := xml.Unmarshal([]byte(raw[strings.Index(raw, "<Task "):]), &task); err != nil {
		t.Fatal(err)
	}
	if task.Command != job.Binary || task.Arguments != `--config "`+job.Config+`" --scheduled run` ||
		task.Boundary != "2026-11-10T21:35:00+08:00" || task.User != "S-1-5-21-10" {
		t.Fatalf("unexpected task: %+v", task)
	}

	units := linuxUnits(Job{Binary: "/tmp/daily-log", Config: "/tmp/工作 区/config.yaml", Clock: job.Clock})
	if !strings.Contains(units["daily-log-generate.timer"], "OnCalendar=*-*-* 21:35:00 Asia/Shanghai") ||
		!strings.Contains(units["daily-log-generate.service"], `--config "/tmp/工作 区/config.yaml" --scheduled run`) {
		t.Fatal("systemd unit does not use the saved settings")
	}
}

func TestLinuxUnitsPassSystemdValidation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd validation requires Linux")
	}
	analyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze is unavailable")
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var paths []string
	for name, body := range linuxUnits(Job{Binary: program, Config: filepath.Join(dir, "config.yaml"), Clock: "00:15"}) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	output, err := exec.Command(analyze, append([]string{"verify"}, paths...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("systemd verify: %v\n%s", err, output)
	}
}
