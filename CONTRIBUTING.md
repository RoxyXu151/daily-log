# 贡献指南 · Contributing to Personal Daily Log

感谢关注与参与 `daily-log` 的开发。本文说明代码结构、构建测试方式、界面设计纪律以及新增来源的约定。

## 目录结构

| 目录 | 职责 |
| --- | --- |
| `cmd/daily-log` | CLI 入口、参数解析与运行编排 |
| `cmd/google-oauth` | Google OAuth 桌面端回环 PKCE 首次授权工具 |
| `cmd/build-release` | 按白名单打包干净源码、扫描凭证并构建跨平台发行包 |
| `internal/source` | 各活动来源采集器（Zotero、Todoist、Calendar、GitHub、GitLab、WakaTime、ActivityWatch、本地笔记目录等） |
| `internal/codex` | Codex 会话只读提取、轮次脱敏与正则分类规则 |
| `internal/journal` | 三时段日记事项提炼、证据校验、阅读会话推断与分类统计 |
| `internal/store` | SQLite 存储、水位推进、历史快照与备份 |
| `internal/tables` | Google Sheets 与本地 Excel 原生增量更新、人工内容保护与读回核验 |
| `internal/mobilehost` / `internal/mobileapi` | Android HTTPS 移动访问配对、会话与版本冲突协议 |
| `desktop/` | 基于 `github.com/egoist/mygo` 的原生跨平台桌面与 WebAssembly 界面（独立 Go 模块） |
| `scheduler/` | Linux `systemd --user` 与 Windows `Task Scheduler` 安装卸载脚本 |
| `docs/` | 架构、配置、同步语义与界面设计系统文档 |
| `site/` | GitHub Pages 静态展示页资源 |

## 开发环境

- **核心模块（根目录）**：Go `1.24+`，纯 Go SQLite（`CGO_ENABLED=0` 可直接跨平台编译）。
- **桌面模块（`desktop/`）**：Go `1.27.3` + [MyGo](https://github.com/egoist/mygo) `v0.8.3`；Linux 运行桌面窗口需系统安装 GTK3。

## 运行测试与检查

在提交修改前，请在仓库根目录通过以下全部检查：

```sh
# 1. 核心模块静态检查、单元测试与离线演示验证
go vet ./...
go test ./...
go run ./cmd/daily-log demo

# 2. 桌面模块测试与编译检查
go -C desktop vet ./...
go -C desktop test ./...

# 3. 发行白名单与凭证泄漏扫描测试
go test ./cmd/build-release
```

## 界面修改与视觉回归

`desktop/` 遵循严格的五级面阶梯、Oklab 色温掺入与统一动效词汇，修改界面前请先阅读 [界面设计系统](docs/design.md)：

- 不引入 `desktop/tokens.go` 之外的字号、圆角或间距字面量。
- 卡片依靠面色阶梯与向光发丝线（`edgeLit`）区分层次，不为卡片加阴影或彩色描边。
- 异步加载、后台进度与错误提示不得推动正文布局。

修改界面后，可生成浅色／深色及窄屏（420px）快照人工核验版面：

```sh
cd desktop && DAILY_LOG_VISUAL_DIR=/tmp/daily-log-visual go test -run TestVisualSnapshots .
DAILY_LOG_ANIM_DIR=/tmp/daily-log-anim go test -run TestAnimationFrames ./desktop
```

## 新增数据来源

若接入具备公开文档或标准本地接口的常见服务，优先按目录来源（Catalog Source）实现：

1. 在 `internal/config/source_fields.go` 的 `builtInSources` 登记字段说明与 `SecretKey`。
2. 在 `internal/source/catalog.go` 实现 `Catalog` 接口（`Check` 返回面向人类的验证说明，`Collect` 返回时间窗口内的 `model.Event` 批次与稳定 ID）。
3. 在 `internal/runtimeapp/catalog.go` 完成装配，并在 `internal/source/collectors_test.go` 补充使用 `httptest` 或临时目录的离线单元测试。

## 隐私与安全红线

- 严禁在代码、测试样例、文档或截图固定数据中写入真实 API Key、OAuth 令牌、本机隐私路径或个人账号。
- 所有时间与证据须忠于来源原始记录：无可靠起止证据时不得推算或编造活跃时长，人工编辑内容与表格公式在任何重新采集或同步中必须受保护。
