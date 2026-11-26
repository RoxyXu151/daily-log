# 个人 AI 日志 · Personal Daily Log

[![Test](https://github.com/RoxyXu151/daily-log/actions/workflows/test.yml/badge.svg)](https://github.com/RoxyXu151/daily-log/actions/workflows/test.yml)
[![Pages](https://github.com/RoxyXu151/daily-log/actions/workflows/pages.yml/badge.svg)](https://roxyxu151.github.io/daily-log/)
[![Go](https://img.shields.io/badge/Go-1.24%2B%20%7C%201.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-2ea44f.svg)](LICENSE)

把 Zotero 阅读标注、Todoist 完成任务、Codex 对话、代码仓库与本地笔记活动整理成**证据可追溯的三时段日记**。Go 版 `0.3.0` 默认在本机保存 SQLite 并导出 Markdown 与 JSON，支持原生桌面工作台、Android 浏览器安全配对以及 Google Sheets / Excel 无损同步，源码按 [MIT](LICENSE) 分发。

👉 **[在线产品主页与界面预览 (GitHub Pages)](https://roxyxu151.github.io/daily-log/)**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="site/assets/dark-dashboard-wide.png">
  <source media="(prefers-color-scheme: light)" srcset="site/assets/light-dashboard-wide.png">
  <img alt="Personal Daily Log 桌面工作台界面预览" src="site/assets/light-dashboard-wide.png" width="100%">
</picture>

## 核心特点

- **证据驱动，拒绝臆造**：AI 将同一天的多轮对话、阅读标注与任务完成记录归并为不超过 28 字的高价值事项与具体进展说明，保留原始证据引用；缺少起止证据时保持时长未知。
- **本地优先与脱敏保护**：单可执行文件运行，SQLite（WAL + 文件锁）本地存储；新安装默认关闭所有外部来源。凭证隔离保存在 `.env` 或 `*.settings.json`（`0600`），同步与提取前自动遮盖已知密钥。
- **人工内容受保护**：手动编辑的日记不会被后续自动整理覆盖；Google Sheets 与本地 Excel 原生增量更新，保留人工修改、公式、格式与未知来源非空行。
- **水位补采与无人值守**：按每个来源的最后成功水位回查，关机或单源临时失败后可通过 `recover` 或 `systemd` / `Task Scheduler` 登录自动补齐。
- **三端一致体验**：命令行、基于 [MyGo](https://github.com/egoist/mygo) 的 Linux / Windows 原生桌面界面与 Android HTTPS 浏览器访问共享同一套数据语义与并发版本对照。

## 快速开始

使用 Go `1.24` 或更新版本从源码构建核心 CLI：

```sh
go build -trimpath -o dist/daily-log ./cmd/daily-log
./dist/daily-log --version
./dist/daily-log demo
```

Windows 将构建目标改为 `dist/daily-log.exe`。`demo` 仅使用合成记录，默认输出到独立的 `daily-log-demo/journals/`，无需任何账号或模型密钥；同一日期重复演示保持幂等。

在专用目录开始真实日志：

```sh
daily-log init
daily-log doctor
daily-log run
```

整理每日事项需要配置兼容 OpenAI Chat Completions 协议的默认模型（如 DeepSeek、OpenAI、Ollama 等）：未配置时提示「未配置模型」；模型临时失败时保留原日记与待整理标记，下次运行自动重试。升级或切换模型后可用 `daily-log generate --date YYYY-MM-DD` 重整指定日期，或用 `daily-log generate --all` 重整全部缓存日期。

## 连接来源与输出

新安装默认关闭所有外部来源，按需在桌面端「来源管理 → 添加来源」或配置文件中启用：

| 类别 | 名称 | 接入方式与说明 |
| --- | --- | --- |
| 文献与日程 | **Zotero** | 本机只读 API（默认 `http://localhost:23119/api`），采集标注、笔记并推断阅读会话 |
| 文献与日程 | **Todoist** | 官方 API（`TODOIST_API_TOKEN`），采集本人完成事件并提示免费版历史范围限制 |
| 文献与日程 | **Google Calendar** | OAuth 令牌或服务账号只读日历，展开重复日程并识别取消事件 |
| AI 与代码 | **Codex** | 通过本机或远程 SSH 主机的 `codex app-server` 只读接口按完整轮次提取实质讨论 |
| AI 与代码 | **GitHub / GitLab** | 官方 REST API（`GITHUB_TOKEN` / `GITLAB_TOKEN`），采集推送、PR/MR、议题与评审 |
| AI 与代码 | **WakaTime** | 官方 API（`WAKATIME_API_KEY`），按天与项目汇总实际编码时长 |
| 本机活动与笔记 | **ActivityWatch** | 本机 HTTP API（`http://127.0.0.1:5600`），读取窗口与浏览器活跃记录 |
| 本机活动与笔记 | **Obsidian / Logseq** | 本地 Vault / 图谱目录扫描，按修改时间记录 Markdown 与 Org 笔记编辑，自动排除导出目录 |
| 导出与同步 | **Markdown / JSON** | 本地文件导出，默认启用 |
| 导出与同步 | **Google Sheets / Excel** | Go 进程内原生读写（无需外部运行库），校验表头与唯一键，保护人工内容与公式 |

详细授权与字段说明见 [配置与授权](docs/configuration.md) 及 [Google 首次授权](docs/google-setup.md)。

## 日常命令

```sh
daily-log run                         # 依次采集、整理当天日志并同步输出
daily-log status                      # 检查各来源采集水位、覆盖范围与任务状态
daily-log recover                     # 按保存的水位补采、补生成并同步
daily-log show YYYY-MM-DD             # 在终端查看指定北京时间日期的日记
daily-log --config config.yaml backup # 升级前安全备份 SQLite 数据库至 daily-log-backups/
```

## 桌面工作台

桌面版使用 MyGo 原生 UI 绘制，遵循五级面阶梯与 Oklab 色温设计体系（详见 [界面设计系统](docs/design.md)）。从源码构建桌面端需要 Go `1.27.3`（Linux 运行需系统安装 GTK3）：

```sh
go -C desktop build -o ../dist/daily-log-gui .
./dist/daily-log-gui --config config.yaml
```

- **今日工作台与时间轴**：以真实起止证据绘制全天分类时间带（`dayRibbon`），一屏呈现今日高价值事项、来源健康度与导出结果。
- **日记总表与并排编辑**：按日期概览事项与分类统计，点击展开上午／下午／晚上三时段事项并直接下钻原始证据；宽屏编辑器支持正文与证据并排核对。
- **来源、分类与多模型管理**：界面内直接测试连接、单独触发采集、增删重命名分类（自动迁移历史活动），以及管理多个本地／云端 OpenAI 兼容模型。
- **系统定时任务**：配置每日整理时刻（默认北京时间 `23:00`）与回查天数，一键安装或更新 Linux `systemd` 用户定时器或 Windows 计划任务。

## Android 浏览器安全连接

Android 手机可通过浏览器连接 Linux 或 Windows 采集主机，共用同一套界面与本地数据库：

```sh
go build -trimpath -o dist/daily-log ./cmd/daily-log
(cd desktop && mygo build -platform web)
./dist/daily-log --config config.yaml mobile --listen 192.168.1.20:8443 \
  --tls-cert /path/to/host.crt --tls-key /path/to/host.key
```

也可直接在桌面端「手机配对」页填写主机地址与受手机信任的 HTTPS 证书后启用。配对码有效 10 分钟，连接会话有效 12 小时；支持断网原页重试与多端同时保存时的版本冲突对照。

## 文档索引

- [配置与授权](docs/configuration.md) · [Google 首次授权](docs/google-setup.md)
- [架构和数据语义](docs/architecture.md) · [日记事项规则](docs/journal-rules.md) · [每日活动占比](docs/activity-mix.md)
- [Google Sheets 与 Excel 输出](docs/google-sheets-sync.md) · [本地 Excel 工作流](docs/local-excel.md) · [Codex 会话采集](docs/codex-activity.md)
- [界面设计系统](docs/design.md) · [图形化版产品需求](docs/gui-requirements.md)
- [定时部署](docs/deployment.md) · [日常维护](docs/maintenance.md) · [验证方式](docs/acceptance.md) · [公开发布](docs/public-release.md)

## 开发与参与贡献

```sh
go vet ./... && go test ./...
go -C desktop vet ./... && go -C desktop test ./...
go run ./cmd/build-release
```

欢迎提交 Issue 或 Pull Request，参与前请阅读 [贡献指南](CONTRIBUTING.md) 与 [安全策略](SECURITY.md)。本项目基于 [MIT License](LICENSE) 开源。
