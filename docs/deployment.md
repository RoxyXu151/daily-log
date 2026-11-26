# 定时部署

先构建 Go 可执行文件到 `dist/daily-log`（Windows 为 `dist/daily-log.exe`），完成凭证配置并手动通过 `doctor`、`run`。定时任务直接调用可执行文件。机器关闭期间不要求运行程序；系统恢复后通过持久化计时器/登录任务检查本地水位补采。

## Linux systemd 用户任务

```bash
bash scheduler/linux/install.sh
systemctl --user list-timers 'daily-log-*'
systemctl --user status daily-log-recover.service
```

安装器创建到 `~/.config/systemd/user/`，升级时自动停用并移除旧 collect.timer，避免重复定时采集：

| 单元 | 行为 |
| --- | --- |
| daily-log-generate.timer | 每天 23:00:00 Asia/Shanghai，Persistent=true；触发 run 依次采集、总结当天并同步 |
| daily-log-recover.service | 用户 systemd 启动时检查欠账，安装时也执行一次 |
| daily-log-generate.service | 执行 daily-log run，失败 15 分钟后重试 |
| daily-log-collect.service | 保留手动采集入口，不再有独立计时器 |

`Persistent=true` 在用户计时器重新激活后补跑错过的触发，不会把每一天都独立执行一次；程序本身按最后成功水位扩大回查以覆盖跨天欠账。**没有每五分钟采集服务，也没有常驻守护进程。** 15 分钟重试只在上一次任务失败时生效，成功后结束。

用户管理器通常从登录开始。若希望开机但尚未登录时就可执行，可由用户选择 `loginctl enable-linger "$USER"`；此时 Zotero 仍必须在同一用户会话中可用，否则保持欠账并等待失败重试。机器关机时无法采集，不会要求它整夜开机。

默认任务单次最长 1 小时。大库可在生成的 service 中调整 `TimeoutStartSec`，随后 `systemctl --user daemon-reload`。更换项目目录或可执行文件路径后重新运行安装器。

```bash
journalctl --user -u daily-log-collect.service -u daily-log-generate.service -u daily-log-recover.service -n 100
tail -n 100 logs/daily-log.log
systemctl --user start daily-log-recover.service
bash scheduler/linux/uninstall.sh
```

卸载仅删除对应单元，保留数据库、凭证和源码。安装器支持用 `DAILY_LOG_BIN=/absolute/daily-log` 指定可执行文件。

## Windows Task Scheduler

在项目目录的 PowerShell 中：

```powershell
powershell -ExecutionPolicy Bypass -File scheduler/windows/install.ps1
Get-ScheduledTask -TaskName 'PersonalDailyLog-*'
Get-ScheduledTaskInfo -TaskName 'PersonalDailyLog-generate'
Start-ScheduledTask -TaskName 'PersonalDailyLog-recover'
Get-Content logs/daily-log.log -Tail 100
```

安装两个当前用户任务：`PersonalDailyLog-generate`（执行 `run`）和 `PersonalDailyLog-recover`。安装时移除旧的独立 collect 任务。XML 中 23:00 的 `StartBoundary` 显式使用 `+08:00`，与主机显示时区分离。登录触发恢复任务，日常任务设置 `StartWhenAvailable` 补跑、失败后每 15 分钟重试（最多 96 次）。没有配置每五分钟采集。

任务采用 `InteractiveToken`，只在本人已登录时运行，不保存 Windows 登录密码。关机/未登录期间的工作在之后登录时恢复。无需勾选唤醒电脑。系统文件锁保证任务不会同时修改缓存，夜间任务依次采集、整理和同步；若仍有来源失败，退出码为 1，计划任务稍后重试。

可通过 `-Binary 'C:\absolute path\daily-log.exe'` 指定可执行文件。卸载：

```powershell
powershell -ExecutionPolicy Bypass -File scheduler/windows/uninstall.ps1
```

首次在目标 Windows 环境部署后，建议用上述命令核对任务注册、北京时间触发边界、登录恢复和退出码。

每天 23:00 触发时总结当天截至执行时已采集到的活动，完成采集后立即生成并同步，无需等到 23:10。23:00 之后的新增记录会在后续回查中补充；当天与最近三天都会重新核对。
