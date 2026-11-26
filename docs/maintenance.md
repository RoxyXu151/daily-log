# 日常维护

用 `daily-log status` 查看来源水位和输出状态，`daily-log doctor` 检查连接，`daily-log recover` 补采和补生成。表格同步失败可用 `daily-log sync` 重试。

在桌面或移动界面直接核对、编辑并保存日记，或用 `daily-log show YYYY-MM-DD` 在终端查阅。表格中的人工修改、公式及无写入快照的非空行均自动保留，候选更新写入本地 SQLite `suggestions` 队列，不覆盖人工内容。

备份数据库使用 `daily-log backup`。SQLite 开启 WAL，不应在写入时只复制主数据库文件。配置、`.env`、凭证和 `*.settings.json` 需要分别备份。

OAuth 刷新失败时重新运行 `go run ./cmd/google-oauth`。表头缺失或重复键时按 [表头定义](sheets-schema.json) 人工核对。Excel 被占用时保存并关闭后重试。
