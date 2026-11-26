package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"daily-log/internal/app"
	"daily-log/internal/config"
	"daily-log/internal/model"
	"daily-log/internal/runtimeapp"
	"daily-log/internal/source"
	"daily-log/internal/store"
)

const version = "0.3.0"

const usage = `用法：daily-log [--config 文件] <命令> [参数]

  init                            创建本地配置
  demo [--directory 路径]         运行离线演示
  collect [--history] [--since 日期]
  generate [--date 日期 | --all]  整理日记
  run                             依次采集、整理、同步
  recover                         补采、补生成与同步
  sync [--inspect]                同步或只读检查表格结构
  status                          查看水位与任务状态
  backup                          在升级前备份本地数据库
  show 日期                       查看本地日记
  doctor                          检查配置与 API
  mobile --tls-cert 文件 --tls-key 文件  启用 HTTPS 移动访问与配对
  --version                       查看版本

日期格式：YYYY-MM-DD（北京时间）
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

type options struct {
	config, command, directory, day, since string
	listen, assets, certificate, key       string
	history, all, inspect                  bool
}

func flags(name string, stderr io.Writer) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(stderr)
	f.Usage = func() {
		fmt.Fprint(stderr, usage)
		f.PrintDefaults()
	}
	return f
}

func parse(args []string, stderr io.Writer) (options, error) {
	var o options
	global := flags("daily-log", stderr)
	global.StringVar(&o.config, "config", "", "配置文件")
	showVersion := global.Bool("version", false, "查看版本")
	if err := global.Parse(args); err != nil {
		return o, err
	}
	if *showVersion {
		o.command = "version"
		return o, nil
	}
	rest := global.Args()
	if len(rest) == 0 {
		global.Usage()
		return o, errors.New("请指定命令")
	}
	o.command = rest[0]
	local := flags(o.command, stderr)
	positionalDate := false
	switch o.command {
	case "demo":
		local.StringVar(&o.directory, "directory", "daily-log-demo", "离线演示输出路径")
	case "collect":
		local.BoolVar(&o.history, "history", false, "导入现存 Zotero 标注和笔记")
		local.StringVar(&o.since, "since", "", "扩大回查起始日期")
	case "generate":
		local.BoolVar(&o.all, "all", false, "重整全部缓存日期")
		local.StringVar(&o.day, "date", "", "指定整理日期")
	case "sync":
		local.BoolVar(&o.inspect, "inspect", false, "只读检查表格结构")
	case "show":
		positionalDate = true
	case "mobile":
		local.StringVar(&o.listen, "listen", defaultMobileListen(), "HTTPS 监听地址")
		local.StringVar(&o.assets, "assets", defaultMobileAssets(), "MyGo 浏览器构建目录")
		local.StringVar(&o.certificate, "tls-cert", "", "HTTPS 证书")
		local.StringVar(&o.key, "tls-key", "", "HTTPS 私钥")
	case "init", "run", "recover", "status", "backup", "doctor":
	default:
		return o, fmt.Errorf("未知命令 %q", o.command)
	}
	tail := rest[1:]
	if positionalDate && len(tail) > 0 && !strings.HasPrefix(tail[0], "-") {
		o.day, tail = tail[0], tail[1:]
	}
	if err := local.Parse(tail); err != nil {
		return o, err
	}
	if positionalDate && o.day == "" && local.NArg() == 1 {
		o.day = local.Arg(0)
	} else if local.NArg() != 0 {
		return o, errors.New("命令包含多余参数")
	}
	if positionalDate && o.day == "" {
		return o, errors.New("请指定日期")
	}
	for _, day := range []string{o.day, o.since} {
		if day != "" {
			if _, err := model.ParseDay(day); err != nil {
				return o, fmt.Errorf("日期 %q 无效，请使用 YYYY-MM-DD", day)
			}
		}
	}
	return o, nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	o, err := parse(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if o.command == "version" {
		fmt.Fprintln(stdout, version)
		return 0
	}
	result, failed, err := execute(ctx, o, stdout, stderr)
	if result != nil {
		data, encodeErr := model.PrettyJSON(result)
		if encodeErr == nil {
			_, encodeErr = stdout.Write(data)
		}
		err = errors.Join(err, encodeErr)
	}
	if err != nil {
		fmt.Fprintln(stderr, "执行失败：", err)
	}
	if failed || err != nil {
		return 1
	}
	return 0
}

func execute(ctx context.Context, o options, stdout, stderr io.Writer) (any, bool, error) {
	if o.command == "init" {
		if o.config == "" {
			o.config = "config.yaml"
		}
		path, err := config.Init(o.config)
		if err == nil {
			fmt.Fprintln(stdout, "已创建配置："+path)
		}
		return nil, false, err
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	if o.command == "demo" {
		result, err := demo(ctx, o.directory, logger)
		return result, false, err
	}
	if o.config == "" {
		path, err := config.DefaultPath()
		if err != nil {
			return nil, false, err
		}
		o.config = path
	}
	if o.command == "mobile" {
		return nil, false, serveMobile(ctx, o, stdout, stderr)
	}
	cfg, err := config.Load(o.config)
	if err != nil {
		return nil, false, err
	}
	if o.command == "backup" {
		path, err := store.Backup(ctx, cfg.Database)
		if err == nil {
			fmt.Fprintln(stdout, "已备份数据库："+path)
		}
		return nil, false, err
	}
	unlock, err := store.Lock(ctx, cfg.Database)
	if err != nil {
		return nil, false, err
	}
	defer unlock()
	logFile, err := runtimeapp.OpenLog(cfg.LogFile)
	if err != nil {
		return nil, false, err
	}
	defer logFile.Close()
	logger = slog.New(slog.NewTextHandler(io.MultiWriter(stderr, logFile), nil))
	defer func() {
		if ctx.Err() != nil {
			logger.Warn("运行中断")
		}
	}()
	db, err := store.Open(cfg.Database)
	if err != nil {
		return nil, false, err
	}
	defer db.Close()
	service, checks, err := runtimeapp.Configure(cfg, db, logger)
	if err != nil {
		return nil, false, err
	}
	at := time.Now().In(model.Shanghai).Truncate(time.Microsecond)
	result, failed, err := dispatch(ctx, o, at, service, checks)
	return result, failed, service.Privacy.Err(err)
}

func dispatch(ctx context.Context, o options, at time.Time, s app.Service, checks runtimeapp.Integrations) (any, bool, error) {
	switch o.command {
	case "collect":
		var since time.Time
		if o.since != "" {
			since, _ = model.ParseDay(o.since)
		}
		collection, err := s.Collect(ctx, at, source.Window{Since: since, History: o.history})
		if err != nil {
			return nil, false, err
		}
		outputs, err := s.Sync(ctx, at)
		return model.Object{"collection": collection, "outputs": outputs}, !s.RequiredOK(collection), err
	case "generate":
		ready, err := s.Ready(ctx, at, false)
		if err != nil {
			return nil, false, err
		}
		if !ready {
			if _, err := s.Collect(ctx, at, source.Window{}); err != nil {
				return nil, false, err
			}
		}
		journals, err := s.Generate(ctx, at, o.day, o.all)
		if err != nil {
			return nil, false, err
		}
		outputs, err := s.Sync(ctx, at)
		return model.Object{"journals": journals, "outputs": outputs}, false, err
	case "run", "recover":
		var report app.Report
		var err error
		if o.command == "run" {
			report, err = s.Collect(ctx, at, source.Window{})
			if err != nil {
				return nil, false, err
			}
		}
		result, report, err := s.Recover(ctx, at, report)
		return result, !s.RequiredOK(report), err
	case "sync":
		if o.inspect {
			result, err := checks.Inspect(ctx)
			return result, false, err
		}
		result, err := s.Sync(ctx, at)
		return result, false, err
	case "status":
		result, err := s.DB.Status(ctx)
		if err != nil {
			return nil, false, err
		}
		result["enabled"] = checks.Enabled()
		return s.Privacy.Object(result), false, nil
	case "show":
		record, err := s.DB.Journal(ctx, o.day)
		if err == nil && record.Payload == nil {
			return model.Object{"message": "此日期暂无日记"}, false, nil
		}
		record.Payload = s.Privacy.Object(record.Payload)
		return record, false, err
	case "doctor":
		result, err := checks.Doctor(ctx)
		return result, false, err
	}
	return nil, false, fmt.Errorf("未知命令 %q", o.command)
}

func demo(ctx context.Context, directory string, logger *slog.Logger) (model.Object, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(root, "demo.sqlite3")
	unlock, err := store.Lock(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	defer unlock()
	db, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	at := time.Now().In(model.Shanghai).Truncate(time.Microsecond)
	day := model.Day(at)
	date := day.Format(model.DateLayout)
	taskID := model.StableID("demo", date, "task")
	chatID := model.StableID("demo", date, "chat")
	err = db.Transaction(ctx, func(tx *store.Tx) error {
		for _, item := range []struct {
			source, id, key, kind, title, evidence, category string
			hour                                             int
		}{
			{"todoist", taskID, "task", "task_completed", "整理公开演示资料", "completed", model.Career, 9},
			{"codex", chatID, "chat", "conversation_activity", "讨论论文实验设计", "direct", model.Research, 14},
		} {
			event := model.NewEvent(item.id, item.source, item.key, item.kind, item.title, day.Add(time.Duration(item.hour)*time.Hour))
			event.Description, event.Category, event.Evidence = "合成演示数据", item.category, item.evidence
			event.Metadata = model.Object{"important": true, "conversation_only": item.source == "codex"}
			if _, err := tx.UpsertEvent(ctx, event, at); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s := app.Service{DB: db, Logger: logger, AIConfig: map[string]any{"enabled": false}, Outputs: []app.Output{runtimeapp.MarkdownOutput(db, filepath.Join(root, "journals"))}}
	// The demo is synthetic end to end: the events are made up, and so is
	// the digest a model would return for them. Rule-assembled prose is
	// gone, and a demo should not need an API key.
	s.Digest = func(context.Context, string, []model.Event, []string) (model.DailyDigest, error) {
		return model.DailyDigest{
			Summary: "完成公开演示资料的整理与核对，并围绕论文实验设计展开讨论，确定下一步的数据与对比方案。",
			CategorySummaries: []model.CategoryDigest{
				{Category: model.Career, Summary: "整理并核对公开演示资料，补齐说明与示例，形成可发布的版本。", ItemCount: 1},
				{Category: model.Research, Summary: "讨论论文实验设计，明确变量、对比基线与数据采集安排。", ItemCount: 1},
			},
			WorkItems: []model.JournalTopic{
				{ID: model.StableID("demo-topic", date, "task"), Title: "整理公开演示资料",
					Summary: "核对演示内容与示例，补齐说明，形成可发布版本。",
					Period:  "上午", Category: model.Career, Source: "todoist",
					EventCount: 1, Completed: 1, Evidence: []string{taskID}},
				{ID: model.StableID("demo-topic", date, "chat"), Title: "设计论文实验方案",
					Summary: "确定实验变量、对比基线与数据采集安排。",
					Period:  "下午", Category: model.Research, Source: "codex",
					EventCount: 1, Evidence: []string{chatID}},
			},
		}, nil
	}
	if _, err = s.Generate(ctx, at, date, false); err != nil {
		return nil, err
	}
	outputs, err := s.Sync(ctx, at)
	return model.Object{"mode": "synthetic-demo", "date": date, "outputs": outputs}, err
}
