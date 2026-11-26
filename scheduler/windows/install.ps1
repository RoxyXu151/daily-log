param([string]$Binary = "")
$ErrorActionPreference = "Stop"
$ProjectDir = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
if (-not $Binary) { $Binary = Join-Path $ProjectDir "dist/daily-log.exe" }
if (-not (Test-Path $Binary -PathType Leaf)) { throw "请先构建 dist/daily-log.exe，或传入 -Binary 完整路径" }
$Binary = (Resolve-Path $Binary).Path
$Identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
function EscapeXml([string]$Text) { return [System.Security.SecurityElement]::Escape($Text) }
$BinXml = EscapeXml $Binary
$DirXml = EscapeXml $ProjectDir
$UserXml = EscapeXml $Identity
$ConfigXml = EscapeXml (Join-Path $ProjectDir "config.yaml")
$BeijingDate = [TimeZoneInfo]::ConvertTimeBySystemTimeZoneId([DateTimeOffset]::UtcNow, "China Standard Time").ToString("yyyy-MM-dd")
$LegacyTask = Get-ScheduledTask -TaskName "PersonalDailyLog-collect" -ErrorAction SilentlyContinue
if ($LegacyTask) {
    Unregister-ScheduledTask -TaskName $LegacyTask.TaskName -Confirm:$false
}
foreach ($Command in @("generate", "recover")) {
    $ActionCommand = if ($Command -eq "generate") { "run" } else { $Command }
    if ($Command -eq "recover") {
        $Trigger = "<LogonTrigger><Enabled>true</Enabled><UserId>$UserXml</UserId><Delay>PT1M</Delay></LogonTrigger>"
    } else {
        $Clock = "23:00:00"
        $Trigger = "<CalendarTrigger><StartBoundary>${BeijingDate}T${Clock}+08:00</StartBoundary><Enabled>true</Enabled><ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay></CalendarTrigger>"
    }
    $Xml = @"
<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>个人日志：北京时间每日采集与补跑</Description></RegistrationInfo>
  <Triggers>$Trigger</Triggers>
  <Principals><Principal id="Author"><UserId>$UserXml</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable><RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <Enabled>true</Enabled><ExecutionTimeLimit>PT1H</ExecutionTimeLimit>
    <RestartOnFailure><Interval>PT15M</Interval><Count>96</Count></RestartOnFailure>
  </Settings>
  <Actions Context="Author"><Exec><Command>$BinXml</Command>
    <Arguments>--config &quot;$ConfigXml&quot; $ActionCommand</Arguments><WorkingDirectory>$DirXml</WorkingDirectory>
  </Exec></Actions>
</Task>
"@
    Register-ScheduledTask -TaskName "PersonalDailyLog-$Command" -Xml $Xml -Force | Out-Null
}
Write-Host "已安装每天 23:00 采集、总结当天日志及登录补跑。采用北京时间边界，不保存账户密码。"
Get-ScheduledTask -TaskName "PersonalDailyLog-*"
