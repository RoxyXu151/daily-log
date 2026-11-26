# 验证方式

核心与桌面分别是独立 Go 模块，在项目根目录运行：

```sh
go test ./...
go vet ./...
go -C desktop test ./...
go -C desktop vet ./...
go build -trimpath -o dist/daily-log ./cmd/daily-log
./dist/daily-log demo
```

表格回归覆盖凭证脱敏、重复键拒绝、重复同步不写入、人工行保护、原始文本不解析为公式、Excel 原有公式和样式保留、占用文件拒绝、Sheets RAW 批量写入及读回失败不推进快照。Sheets 使用模拟服务，不修改真实远端工作簿。

桌面和浏览器交互验收见 [产品需求](gui-requirements.md)。真实 Google 凭证、系统定时触发和各目标平台仍应在部署环境检查，自动测试不代表外部服务已经连通。
