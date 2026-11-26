// build-release stages only allowlisted sources and builds self-contained releases.
package main

import (
	"archive/zip"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var secret = regexp.MustCompile(`(?m)(\bsk-[A-Za-z0-9_-]{24,}|\bgh[pousr]_[A-Za-z0-9]{20,}|^-----BEGIN [A-Z ]*PRIVATE KEY-----$)`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func command(root string, env []string, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = root
	c.Env = append(os.Environ(), env...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}
func stage(root, target string) error {
	files := []string{
		"README.md", "LICENSE", "CONTRIBUTING.md", "SECURITY.md", ".env.example", ".gitignore", ".gitattributes", "go.mod", "go.sum",
		"desktop/go.mod", "desktop/go.sum", "desktop/mygo.json", "desktop/logo.svg", "desktop/logo.png",
	}
	for _, dir := range []string{"cmd", "internal", "desktop", "scheduler", "docs", ".github"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			ext := filepath.Ext(path)
			switch ext {
			case ".go", ".sql", ".yaml", ".yml", ".sh", ".ps1", ".md", ".json":
				files = append(files, rel)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, rel := range files {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		path := filepath.Join(root, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("发布源文件必须是普通文件：%s", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if secret.Match(data) {
			return fmt.Errorf("发布源文件疑似包含凭证：%s", rel)
		}
		out := filepath.Join(target, rel)
		if err = os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return err
		}
		if err = os.WriteFile(out, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}
func archive(path, root, prefix string, files []string) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	z := zip.NewWriter(out)
	var result error
	for _, rel := range files {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			result = err
			break
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			result = err
			break
		}
		h.Name = filepath.ToSlash(filepath.Join(prefix, rel))
		h.Method = zip.Deflate
		w, err := z.CreateHeader(h)
		if err != nil {
			result = err
			break
		}
		f, err := os.Open(filepath.Join(root, rel))
		if err != nil {
			result = err
			break
		}
		_, err = io.Copy(w, f)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			result = errors.Join(err, closeErr)
			break
		}
	}
	return errors.Join(result, z.Close(), out.Close())
}
func all(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}
func run() error {
	output := flag.String("output", "dist/public", "输出目录")
	desktop := flag.Bool("desktop", false, "同时构建 Linux/Windows 桌面与浏览器资源")
	flag.Parse()
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	destination, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "daily-log-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	staged := filepath.Join(temp, "personal-daily-log")
	if err = stage(root, staged); err != nil {
		return err
	}
	files, err := all(staged)
	if err != nil {
		return err
	}
	if err = archive(filepath.Join(destination, "personal-daily-log-source.zip"), staged, "personal-daily-log", files); err != nil {
		return err
	}
	for _, platform := range []string{"linux", "windows"} {
		variant := platform + "-amd64"
		bundle := filepath.Join(temp, variant)
		if err = os.MkdirAll(bundle, 0755); err != nil {
			return err
		}
		binary := "daily-log"
		if platform == "windows" {
			binary += ".exe"
		}
		if err = command(staged, []string{"GOOS=" + platform, "GOARCH=amd64", "CGO_ENABLED=0"}, "go", "build", "-trimpath", "-o", filepath.Join(bundle, binary), "./cmd/daily-log"); err != nil {
			return err
		}
		for _, rel := range []string{"README.md", "LICENSE"} {
			data, err := os.ReadFile(filepath.Join(staged, rel))
			if err != nil {
				return err
			}
			if err = os.WriteFile(filepath.Join(bundle, rel), data, 0644); err != nil {
				return err
			}
		}
		if err = archive(filepath.Join(destination, "daily-log-cli-"+variant+".zip"), bundle, "daily-log", []string{binary, "README.md", "LICENSE"}); err != nil {
			return err
		}
		if platform == runtime.GOOS {
			data, err := os.ReadFile(filepath.Join(bundle, binary))
			if err != nil {
				return err
			}
			if err = os.WriteFile(filepath.Join(destination, binary), data, 0755); err != nil {
				return err
			}
		}
	}
	if *desktop {
		if err = command(filepath.Join(staged, "desktop"), nil, "mygo", "build", "-skip-installer", "-platform", "web,linux/amd64,windows/amd64"); err != nil {
			return err
		}
		for _, platform := range []string{"linux", "windows"} {
			variant := platform + "-amd64"
			bundle := filepath.Join(staged, "dist", "desktop", variant)
			cli := "daily-log-cli"
			if platform == "windows" {
				cli += ".exe"
			}
			if err = command(staged, []string{"GOOS=" + platform, "GOARCH=amd64", "CGO_ENABLED=0"}, "go", "build", "-trimpath", "-o", filepath.Join(bundle, cli), "./cmd/daily-log"); err != nil {
				return err
			}
			web := filepath.Join(staged, "dist", "desktop", "web")
			webFiles, err := all(web)
			if err != nil {
				return err
			}
			for _, rel := range webFiles {
				data, err := os.ReadFile(filepath.Join(web, rel))
				if err != nil {
					return err
				}
				target := filepath.Join(bundle, "web", rel)
				if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					return err
				}
				if err = os.WriteFile(target, data, 0644); err != nil {
					return err
				}
			}
			for _, rel := range []string{"README.md", "LICENSE"} {
				data, err := os.ReadFile(filepath.Join(staged, rel))
				if err != nil {
					return err
				}
				if err = os.WriteFile(filepath.Join(bundle, rel), data, 0644); err != nil {
					return err
				}
			}
			files, err := all(bundle)
			if err != nil {
				return err
			}
			var included []string
			for _, f := range files {
				if !strings.HasPrefix(f, ".") && !strings.HasSuffix(f, ".deb") && !strings.HasSuffix(f, ".tar.gz") {
					included = append(included, f)
				}
			}
			if err = archive(filepath.Join(destination, "daily-log-"+variant+".zip"), bundle, "daily-log", included); err != nil {
				return err
			}
		}
	}
	fmt.Println(destination)
	return nil
}
