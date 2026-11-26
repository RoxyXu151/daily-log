package privatefs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Restrict checks the file type before tightening permissions on existing data.
func Restrict(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("私有数据路径必须是普通文件：%s", path)
	}
	return os.Chmod(path, 0600)
}

func Open(path string, flags int) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := Restrict(path); err != nil {
		return nil, err
	}
	return os.OpenFile(path, flags, 0600)
}

func Directory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("专用数据目录必须是普通目录：%s", path)
	}
	return os.Chmod(path, 0700)
}

func Write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := Restrict(path); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".daily-log-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func WriteChanged(path string, data []byte) (bool, error) {
	if err := Restrict(path); err != nil {
		return false, err
	}
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	return true, Write(path, data)
}
