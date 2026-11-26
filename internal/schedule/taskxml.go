package schedule

import (
	"context"
	"encoding/binary"
	"fmt"
	"html"
	"os"
	"os/user"
	"path/filepath"
	"time"
	"unicode/utf16"

	"daily-log/internal/model"
)

func installWindows(ctx context.Context, job Job) error {
	account, err := user.Current()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "daily-log-schedule-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	next := model.NextDailyRun(time.Now(), job.Clock)
	for _, action := range []string{"run", "recover"} {
		xml := windowsTask(job, account.Uid, action, next)
		path := filepath.Join(dir, action+".xml")
		runes := utf16.Encode([]rune("\uFEFF" + xml))
		encoded := make([]byte, len(runes)*2)
		for i, r := range runes {
			binary.LittleEndian.PutUint16(encoded[i*2:], r)
		}
		if err := os.WriteFile(path, encoded, 0600); err != nil {
			return err
		}
		name := "PersonalDailyLog-generate"
		if action == "recover" {
			name = "PersonalDailyLog-recover"
		}
		if err := systemCommand(ctx, "schtasks.exe", "/Create", "/TN", name, "/XML", path, "/F"); err != nil {
			return err
		}
	}
	return nil
}

func windowsTask(job Job, sid, action string, next time.Time) string {
	id := html.EscapeString(sid)
	trigger := fmt.Sprintf(`<CalendarTrigger><StartBoundary>%s</StartBoundary><Enabled>true</Enabled><ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay></CalendarTrigger>`,
		next.In(model.Shanghai).Format(time.RFC3339))
	if action == "recover" {
		trigger = `<LogonTrigger><Enabled>true</Enabled><UserId>` + id + `</UserId><Delay>PT1M</Delay></LogonTrigger>`
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Daily Log</Description></RegistrationInfo>
  <Triggers>%s</Triggers>
  <Principals><Principal id="Author"><UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable><Enabled>true</Enabled><ExecutionTimeLimit>PT1H</ExecutionTimeLimit>
    <RestartOnFailure><Interval>PT15M</Interval><Count>96</Count></RestartOnFailure>
  </Settings>
  <Actions Context="Author"><Exec><Command>%s</Command><Arguments>%s</Arguments><WorkingDirectory>%s</WorkingDirectory></Exec></Actions>
</Task>
`, trigger, id, html.EscapeString(job.Binary), html.EscapeString(`--config "`+job.Config+`" --scheduled `+action),
		html.EscapeString(filepath.Dir(job.Config)))
}
