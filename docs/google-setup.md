# Google 首次授权

启用 Google Sheets API，配置 OAuth 同意屏幕，创建 Desktop app 客户端并将下载的 JSON 放到 `secrets/client.json`。

在项目根目录运行：

```sh
go run ./cmd/google-oauth --client secrets/client.json --token secrets/google-token.json
```

在浏览器打开终端打印的链接。程序等待本机回环回调，10 分钟后超时，校验 state 并使用 PKCE。成功后以私有权限保存刷新令牌。默认只请求 Sheets 权限，需要日历时明确添加 `--include-calendar`。

在配置文件同目录的 `.env` 中设置 `GOOGLE_OAUTH_TOKEN=secrets/google-token.json`。设置 `sheets.enabled: true` 与 `sheets.spreadsheet_id`，执行 `daily-log sync --inspect` 检查后再同步。授权工具不会自动启用来源、修改表格或安装定时任务。

也可设置 `GOOGLE_APPLICATION_CREDENTIALS` 指向服务账号 JSON，并将目标工作簿分享给该账号。两种配置同时存在时优先使用服务账号。现有令牌文件可以继续使用，过期时由 Go 客户端刷新。

独立程序使用本地凭证，连接器的授权不会自动提供给系统定时任务。参考 [Google 桌面应用 OAuth 文档](https://developers.google.com/identity/protocols/oauth2/native-app)。
