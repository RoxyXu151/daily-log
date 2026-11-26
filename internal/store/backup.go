package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/privatefs"
)

// Backup opens the existing database without running the schema upgrade.
func Backup(ctx context.Context, path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return "", errors.New("需要已有的 SQLite 数据库文件")
	}
	root := filepath.Join(filepath.Dir(path), "daily-log-backups")
	if err := privatefs.Directory(root); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(root, time.Now().In(model.Shanghai).Format("20060102-150405")+"-")
	if err != nil {
		return "", err
	}
	snapshot, err := saveSnapshot(ctx, path, dir)
	if err != nil {
		_ = os.RemoveAll(dir)
	}
	return snapshot, err
}

func saveSnapshot(ctx context.Context, path, dir string) (string, error) {
	source, err := sql.Open("sqlite", sqliteURI(path, "mode=ro&_pragma=busy_timeout(30000)"))
	if err != nil {
		return "", err
	}
	defer source.Close()
	partial := filepath.Join(dir, "snapshot.partial")
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if _, err := source.ExecContext(ctx, "VACUUM INTO ?", partial); err != nil {
		return "", err
	}
	snapshot := filepath.Join(dir, "daily-log.sqlite3")
	if err := os.Rename(partial, snapshot); err != nil {
		return "", err
	}
	return snapshot, nil
}
