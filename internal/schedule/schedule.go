package schedule

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"daily-log/internal/model"
	"daily-log/internal/privatefs"
)

type Job struct {
	Binary, Config, Clock string
}

func Install(ctx context.Context, job Job) error {
	if !model.ValidDailyClock(job.Clock) {
		return errors.New("定时时间须使用 HH:MM")
	}
	if !filepath.IsAbs(job.Binary) || !filepath.IsAbs(job.Config) || strings.ContainsAny(job.Binary+job.Config, "\r\n\x00") {
		return errors.New("定时任务需要有效的绝对程序和配置路径")
	}
	if info, err := os.Stat(job.Binary); err != nil || !info.Mode().IsRegular() {
		return errors.New("找不到用于定时执行的程序")
	}
	switch runtime.GOOS {
	case "linux":
		return installLinux(ctx, job)
	case "windows":
		return installWindows(ctx, job)
	default:
		return errors.New("当前系统尚无定时任务安装器")
	}
}

func installLinux(ctx context.Context, job Job) error {
	if err := systemCommand(ctx, "systemctl", "--user", "show-environment"); err != nil {
		return err
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(base, "systemd", "user")
	for name, body := range linuxUnits(job) {
		if err := privatefs.Write(filepath.Join(dir, name), []byte(body)); err != nil {
			return err
		}
	}
	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", "daily-log-recover.service"},
		{"enable", "--now", "daily-log-generate.timer"},
		{"restart", "daily-log-generate.timer"},
	} {
		if err := systemCommand(ctx, "systemctl", append([]string{"--user"}, args...)...); err != nil {
			return err
		}
	}
	return nil
}

func linuxUnits(job Job) map[string]string {
	units := map[string]string{}
	for name, action := range map[string]string{"generate": "run", "recover": "recover"} {
		install := ""
		if name == "recover" {
			install = "\n[Install]\nWantedBy=default.target\n"
		}
		units["daily-log-"+name+".service"] = fmt.Sprintf(`[Unit]
Description=Daily Log %s
After=network-online.target
StartLimitIntervalSec=0

[Service]
Type=oneshot
ExecStart=%s --config %s --scheduled %s
Restart=on-failure
RestartSec=15min
TimeoutStartSec=1h
UMask=0077

%s`, name, systemdQuote(job.Binary), systemdQuote(job.Config), action, install)
	}
	units["daily-log-generate.timer"] = fmt.Sprintf(`[Unit]
Description=Daily Log nightly schedule

[Timer]
OnCalendar=*-*-* %s:00 Asia/Shanghai
Persistent=true
AccuracySec=1s
Unit=daily-log-generate.service

[Install]
WantedBy=timers.target
`, job.Clock)
	return units
}

func systemdQuote(value string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$")
	return `"` + r.Replace(value) + `"`
}

func systemCommand(ctx context.Context, command string, args ...string) error {
	output, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s：%w", command, err)
	}
	return fmt.Errorf("%s：%s", command, detail)
}
