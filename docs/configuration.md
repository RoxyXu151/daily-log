# 配置与授权

所有相对路径相对于配置文件目录。根目录 `.env` 由程序加载，现有系统环境变量优先；不要把 token、私钥或密码写进 YAML、源代码或 Git。

## Zotero

默认 `http://localhost:23119/api`、`users/0`，只使用读取 API，不需要 Zotero 云端 API 密钥。也可配置 `groups/<groupID>` 读取本地已同步的群组库。仅访问当前配置的一个库。切换本地库或来源账号时建议使用独立数据库，避免混用历史。

Zotero 每天只在 23:00 做正常采集：一次完整分页扫描现存条目，按来源事件时间过滤并增量 upsert SQLite。这能兼容旧版 Local API 中本地版本号不能可靠反映修改的情形。较大库的一次日扫描成本高于按版本扫描，但无需后台高频轮询。首次完整历史导入用 `collect --history`。

Local API 仍不可用时查看 `doctor`，不要改成每五分钟轮询。程序不启动、不修改 Zotero，也不直接读写其私有数据库。参考 [Zotero Local API 官方文档](https://www.zotero.org/support/dev/web_api/v3/local_api) 和 [读取 API](https://www.zotero.org/support/dev/web_api/v3/basics)。

真实联调发现一个论文条目的 JSON 导出会令整页返回 HTTP 500。采集器因此支持对失败页分拆定位，并对已知论文类型使用该条目的独立 CSL JSON 元数据出口，保留来源格式标记和诊断键。无法完整读取标注/笔记或无法确认是论文元数据时仍整次报错，不推进成功水位。

## 本地 Excel（按需启用）

`excel.enabled: true`，输出为 `outputs/local/个人活动日志.xlsx`。无需 Google 账号授权。运行、审核保护、备份和迁移要求见 [本地 Excel 工作流](local-excel.md)。

## Google Sheets（按需启用）

独立程序与系统定时任务使用本地保存的 OAuth 令牌或服务账号凭证，不依赖外部编辑器会话授权。

1. 在 Google Cloud 项目启用 Google Sheets API（如需采集日历可一并启用 Google Calendar API），配置 OAuth 同意屏幕，创建「桌面应用」OAuth 客户端。
2. 将客户端 JSON 保存到 `secrets/client.json`。
3. 运行 `go run ./cmd/google-oauth --client secrets/client.json --token secrets/google-token.json`，在本机浏览器完成一次本人授权。
4. `.env` 设置 `GOOGLE_OAUTH_TOKEN=secrets/google-token.json`。令牌自动刷新；Google 应用处于外部测试模式时刷新令牌可能短期失效，需要按 Google OAuth 设置处理。
5. 执行 `daily-log sync --inspect`。确认四个表名和 `docs/sheets-schema.json` 一致，再设置 `sheets.enabled: true` 并运行 `daily-log generate`。

默认仅请求 Sheets 读写权限；显式传入 `--include-calendar` 时追加 Calendar 只读权限。参考 [Google Calendar 事件列表](https://developers.google.com/calendar/api/v3/reference/events/list) 和 [Sheets 值写入](https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/batchUpdate)。

替代方案：服务账号 JSON 放入 `secrets/google-service-account.json`，`.env` 设置 `GOOGLE_APPLICATION_CREDENTIALS` 指向该路径，把既有工作簿分享给服务账号邮箱并授予编辑权限。Calendar 则需额外将相应日历分享给该账号，并配置真实日历 ID；服务账号的 `primary` 不代表你的个人日历。两种凭证同时配置时优先服务账号。

凭证文件建议只允许本人读取（Linux `chmod 600`）。`.env`、`secrets/`、数据库和日志均由 `.gitignore` 排除。

## Todoist

在 Todoist 开发者设置获取个人 API token，写入 `.env` 的 `TODOIST_API_TOKEN`，设置 `todoist.enabled: true`。完成记录的分类由规则与 AI 自动判定，无需手工维护项目白名单。

可在桌面来源管理中保存并测试 Todoist 凭证，也可设置 `.env` 和 YAML 后执行 `daily-log doctor` 验证，再运行 `daily-log run`。

采集 `/api/v1/user`、`/projects` 与 `/activities`，使用 `date_from/date_to` 范围、完成事件类型、本人 `initiator_id` 和游标分页。即使服务器过滤生效，也会在本地再次核验操作人。每个任务的描述在完成事件没有携带时，从当前任务快照补充；任务已不可读取则留空，并保留 `description_available` 标识。快照不冒充完成当时的完整内容。

去重键使用本人 ID、任务 ID 和完成时间（毫秒精度），以兼容旧连接器的时间截断；原始微秒时间仍保存在事件和证据中。

按官方定价，Free 当前提供一周活动历史；代码读取账号 `is_premium`，按可配置 `history_days_free: 7` 提示可能的断档。若关机超过保留期，**无法保证找回已被服务端隐藏的事件**。空响应不能证明没有活动，403 不回退为当前待办清单。保存到本地的记录不因云端保留期删除。

参考 [Todoist API 官方文档](https://developer.todoist.com/api/v1/) 和 [官方计划说明](https://www.todoist.com/pricing)。

## Calendar

设置 `calendar.enabled: true`，`calendar_ids` 可列多个日历，OAuth 用户可先用 `primary`。重复日程展开为实例；全天和跨午夜事件保留原始边界；明确取消的事件在本地标记取消，后续摘要不再计入。系统只对配置回查范围内返回的取消做处理，源端超出保留/回查窗口的历史更改可用 `collect --since ...` 扩大回查。

## 添加来源与常见服务

来源管理只列出已添加的来源；右上角「添加来源」打开目录，按标准接入方式列出常见服务，添加后只需填凭证即可测试连接。每个来源的接入方式都是该服务的标准接口：

| 来源 | 标准接入方式 | 需要 |
|---|---|---|
| GitHub | 官方 REST API | `GITHUB_TOKEN`（用户名可留空自动识别） |
| GitLab | 官方 REST API | `GITLAB_TOKEN` |
| WakaTime | 官方 REST API | `WAKATIME_API_KEY` |
| ActivityWatch | 本机 HTTP API | 默认 `http://127.0.0.1:5600`，无需密钥 |
| Obsidian | 本地 Vault 目录 | Vault 路径，按文件修改时间记录 |
| Logseq | 本地图谱目录 | 图谱路径，按文件修改时间记录 |

Zotero、Todoist、Codex、Google Calendar 为默认已添加的来源。目录来源默认非必需：单个来源采集失败不会阻断当天其余来源的整理。密钥只保存在本地 settings 或 `.env`，YAML 不含密钥。

## AI 模型配置

支持任意兼容 OpenAI Chat Completions 协议的服务（如 DeepSeek、OpenAI、Ollama、vLLM 等）。可在桌面端「模型设置」管理多个配置并指定全局默认模型，也可通过 `config.yaml` 与 `.env` 配置：

- `ai.enabled: true`，`ai.base_url` 指向 API 根路径（例如 `https://api.deepseek.com` 或 `http://127.0.0.1:11434/v1`），填写 `ai.model`。
- 密钥保存在忽略提交的 `.env`（`AI_API_KEY`）或桌面端本地 `*.settings.json`（权限 `0600`），无需将密钥写入 YAML。
- `ai.request_options` 可传递供应商兼容选项（例如关闭思考模式、限制最大输出 token 等）以控制日常摘要开销。

开启后会把用于摘要的活动标题和描述发送到所选模型服务。

为防止编造事实，AI 只能从给定描述选择至多 100 字的连续摘录；最终自然语言由证据模板组合。模型不能增加时长、把安排升级成成果、改变来源标签。整理一天必须有模型：没有密钥时运行会直接提示「未配置模型」，AI 失败（网络、限流、非 JSON、新增文字）时报错并把该日留待下次重试，不再生成规则拼凑的占位正文。手动编辑或已确认的日记不需要模型，也不会被覆盖。接口不要求某一家付费服务。
