package runtimeapp

import (
	"fmt"
	"path/filepath"
	"strings"

	"daily-log/internal/config"
	"daily-log/internal/netclient"
	"daily-log/internal/source"
)

// catalogCollector builds one of the standard catalog sources from its saved
// fields and secrets. The four legacy sources keep their bespoke wiring; a
// catalog source is entirely described by its spec, so adding a common
// service needs a spec and a collector, not a new config section.
func catalogCollector(id string, cfg config.Config, client netclient.Client) (source.Catalog, error) {
	values, err := cfg.ConnectionValues(id)
	if err != nil {
		return nil, err
	}
	secret := func(key string) string { return strings.TrimSpace(cfg.SourceSecret(id, key)) }
	switch id {
	case "github":
		return source.GitHub{
			HTTP: client, BaseURL: firstOf(values["base_url"], "https://api.github.com"),
			Username: strings.TrimSpace(values["username"]), Token: secret("GITHUB_TOKEN"),
		}, nil
	case "gitlab":
		return source.GitLab{
			HTTP: client, BaseURL: firstOf(values["base_url"], "https://gitlab.com"),
			Token: secret("GITLAB_TOKEN"),
		}, nil
	case "wakatime":
		return source.WakaTime{
			HTTP: client, BaseURL: firstOf(values["base_url"], "https://wakatime.com/api/v1"),
			Key: secret("WAKATIME_API_KEY"),
		}, nil
	case "activitywatch":
		return source.ActivityWatch{HTTP: client, BaseURL: firstOf(values["base_url"], "http://127.0.0.1:5600")}, nil
	case "obsidian":
		return source.LocalFolder{
			Name: "Obsidian", Root: resolvePath(cfg, values["vault_path"]),
			Extensions: []string{".md"}, Ignore: ignoredDirs(cfg),
		}, nil
	case "logseq":
		return source.LocalFolder{
			Name: "Logseq", Root: resolvePath(cfg, values["graph_path"]),
			Extensions: []string{".md", ".org"}, Ignore: ignoredDirs(cfg),
		}, nil
	}
	return nil, fmt.Errorf("来源 %s 没有标准接入实现", id)
}

// filePathKey is the field a local-folder source reads its directory from.
func filePathKey(id string) string {
	if id == "logseq" {
		return "graph_path"
	}
	return "vault_path"
}

// resolvePath keeps an empty field empty: resolving "" would hand the
// collector the config root and let it read the app's own files as notes.
func resolvePath(cfg config.Config, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return cfg.Resolve(value)
}

// ignoredDirs are the places this app writes its own output; a vault that
// happens to contain them must not read them back as notes.
func ignoredDirs(cfg config.Config) []string {
	dirs := []string{filepath.Dir(cfg.Database), filepath.Dir(cfg.LogFile)}
	if cfg.Markdown.Directory != "" {
		dirs = append(dirs, cfg.Markdown.Directory)
	}
	if cfg.Excel.Path != "" {
		dirs = append(dirs, filepath.Dir(cfg.Excel.Path))
	}
	return dirs
}

func firstOf(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
