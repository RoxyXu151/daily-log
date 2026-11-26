# 本地 Excel 工作流

Excel 由 Go 中的 Excelize 直接读写。无需安装办公软件或其他运行库。

```yaml
excel:
  enabled: true
  path: outputs/local/个人活动日志.xlsx
```

路径相对于配置文件目录。新工作簿创建每日活动、活动流水、论文阅读和统计看板；已有文件按实际表头和稳定键更新，其他页及格式保留。

```sh
daily-log doctor
daily-log sync --inspect
daily-log run
daily-log status
```

人工修改、公式及无写入快照的非空行受保护；新事实进入本地 SQLite 建议队列而不覆盖单元格。日记正文可在桌面或移动端直接编辑保存。

同步前保存并关闭工作簿。检测到 `~$` 占用文件时停止。程序先在内存生成结果，再读回核对变化与原有单元格，检查原文件未被同时修改后备份并原子替换。`.backups` 最多保留五份，没有内容变化时不重写文件。失败不会推进写入快照。

旧配置中的外部运行库路径不再使用。Google Sheets 的凭证与开关独立，Excel 本身不需要 Google 授权。
