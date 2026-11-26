package runtimeapp

import (
	"errors"
	"fmt"
	"os"

	"daily-log/internal/privatefs"
)

func OpenLog(path string) (*os.File, error) {
	if err := privatefs.Restrict(path); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err == nil && info.Size() >= 2_000_000 {
		for i := 3; i > 0; i-- {
			from := path
			if i > 1 {
				from = fmt.Sprintf("%s.%d", path, i-1)
			}
			to := fmt.Sprintf("%s.%d", path, i)
			if err := privatefs.Restrict(from); err != nil {
				return nil, err
			}
			if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
	}
	return privatefs.Open(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY)
}
