//go:build !js

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"daily-log/internal/config"
	"daily-log/internal/mobilehost"
	"daily-log/internal/model"
	"daily-log/internal/privatefs"
	"daily-log/internal/runtimeapp"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	path := flag.String("config", "", "配置文件")
	scheduled := flag.String("scheduled", "", "后台任务（run 或 recover）")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	binary, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	configPath := *path
	if configPath == "" {
		configPath, err = config.DefaultPath()
		if err != nil {
			log.Fatal(err)
		}
	}
	backend := runtimeapp.Workspace{ConfigPath: configPath, SchedulerBinary: binary, Logger: slog.Default()}
	if *scheduled != "" {
		if err := runScheduled(ctx, backend, *scheduled); err != nil {
			log.Fatal(err)
		}
		return
	}
	settings := filepath.Join(filepath.Dir(configPath), "daily-log-appearance.json")
	preferences, appearanceErr := loadAppearance(settings)
	s := screen{
		appearance:    preferences,
		openLink:      mygo.Shell.OpenExternal,
		mobileOptions: mobileAccessOptions{listen: "127.0.0.1:8443", assets: mobilehost.DefaultAssets(binary)},
		startMobile: func(form mobileAccessOptions) (*mobileAccessSession, error) {
			if _, err := config.Load(backend.ConfigPath); err != nil {
				return nil, err
			}
			return startMobileAccess(ctx, backend, form)
		},
		saveAppearance: func(p appearance) error {
			data, err := model.PrettyJSON(p)
			if err != nil {
				return err
			}
			return privatefs.Write(settings, data)
		},
		backend: backend,
		ctx:     ctx,
	}
	defer func() {
		cancel()
		if s.mobileSession != nil {
			<-s.mobileSession.done
		}
	}()
	if appearanceErr != nil {
		log.Printf("读取外观设置失败：%v", appearanceErr)
	}
	configErr := ensureLocalConfig(configPath)
	if configErr != nil {
		s.failed, s.message = true, fmt.Sprintf("无法初始化本地配置：%v", configErr)
	}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "daily-log · 个人活动日志", Width: 1140, Height: 800,
			Content: ui.View(s.view),
		})
		s.dispatch = win.Update
		s.pickMobilePath = func(title string, directory bool) (string, error) {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: "选择" + title, Directory: directory})
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		}
		if configErr == nil {
			s.refresh()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func ensureLocalConfig(path string) error {
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err := config.Init(path)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	return err
}

func runScheduled(ctx context.Context, w runtimeapp.Workspace, action string) error {
	cfg, err := config.Load(w.ConfigPath)
	if err != nil {
		return err
	}
	file, err := runtimeapp.OpenLog(cfg.LogFile)
	if err != nil {
		return err
	}
	defer file.Close()
	w.Logger = slog.New(slog.NewTextHandler(file, nil))
	switch action {
	case "run":
		err = w.Run(ctx)
	case "recover":
		err = w.Recover(ctx)
	default:
		return errors.New("未知后台任务")
	}
	if err != nil {
		w.Logger.Error("定时任务执行失败", "error", err)
	}
	return err
}

func loadAppearance(path string) (appearance, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultAppearance(), nil
	}
	if err != nil {
		return defaultAppearance(), err
	}
	return decodeAppearance(data)
}
