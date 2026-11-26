# Google Sheets 与 Excel 输出

两个输出均由 Go 可执行文件直接执行。SQLite 是事实库，Sheets 与 Excel 是独立的输出视图，表格手工修改不会自动写回 SQLite 或传播到另一份表格。

```sh
daily-log run
daily-log sync --inspect
daily-log sync
daily-log status
```

用 `sheets.enabled`、`excel.enabled` 分别开关。Sheets 必须已有每日活动、活动流水、论文阅读、统计看板四个工作表，字段见 [表头定义](sheets-schema.json)。程序不会重新创建云端工作簿或重置表头。

同步按实际表头与稳定键定位。重复键、缺失字段、歧义表头会在写入前报错。已确认、本人补充、人工修改、公式和无程序快照的非空行均保留，新事实进入 SQLite 建议队列。活动占比是派生列，四个实际分钟列作为输入保留。

Sheets 写前重新读取目标行，以 RAW 写入变化单元格，写后核对实际内容，核验通过后才保存快照。网络失败可再次同步；Google Values API 没有单元格比较交换，写前检查不能彻底消除其他设备同时编辑的竞争窗口。

`sheets.auto_layout: true` 为正文和活动占比启用换行并适应行高，保留列宽。`sheets.activity_colors: true` 按类别着色，保留已有链接和粗体。科研蓝、课程绿、职业橙、生活紫。

首次凭证配置见 [Google 授权](google-setup.md)，本地文件保护见 [Excel 工作流](local-excel.md)。一端失败仍会尝试其他输出，命令最终返回失败以供任务重试。
