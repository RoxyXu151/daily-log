$ErrorActionPreference = "Stop"
foreach ($Command in @("collect", "generate", "recover")) {
    $Task = Get-ScheduledTask -TaskName "PersonalDailyLog-$Command" -ErrorAction SilentlyContinue
    if ($Task) {
        Stop-ScheduledTask -TaskName $Task.TaskName -ErrorAction SilentlyContinue
        Unregister-ScheduledTask -TaskName $Task.TaskName -Confirm:$false
    }
}
Write-Host "定时任务已卸载；项目、数据库与凭证保留。"
