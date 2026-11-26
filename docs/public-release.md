# 公开发布

在项目根目录执行：

```sh
go run ./cmd/build-release
# 同时打包 Linux/Windows 桌面与 Android 浏览器资源，需要安装 MyGo
go run ./cmd/build-release --desktop
```

可用 `--output` 指定输出目录，默认 `dist/public`。构建器在临时目录按允许列表复制 Go 源码、默认配置、文档和部署入口，扫描常见凭证模式，再构建 Linux/Windows amd64 CLI 与源码 ZIP；桌面模式额外调用 MyGo 构建两端与浏览器资源。构建只生成本地文件。

个人 `.env`、`config.yaml`、凭证、数据、日志、导出、已有发行文件和本机依赖不会进入源码包。完整保护行为见 [架构](architecture.md)。

新配置默认关闭外部来源、AI 和表格输出。现有 SQLite 数据结构、事件 ID、日记指纹和写入快照继续兼容。含凭证的新表格内容脱敏，行键含凭证时跳过。
