package source

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"daily-log/internal/model"
	"daily-log/internal/netclient"
)

type Zotero struct {
	HTTP    netclient.Client
	BaseURL string
	Library string
}

type zoteroScan struct {
	Zotero
	headers  http.Header
	total    int
	fallback []string
}

func (z Zotero) endpoint() string {
	return strings.TrimRight(z.BaseURL, "/") + "/" + z.Library + "/items"
}

func (z Zotero) Check(ctx context.Context) error {
	var result []model.Object
	_, err := z.HTTP.JSON(ctx, http.MethodGet, z.endpoint(), url.Values{"limit": {"1"}}, nil, nil, &result)
	return err
}

func (z Zotero) Collect(ctx context.Context, window Window) (Batch, error) {
	scan := zoteroScan{Zotero: z, headers: http.Header{"Zotero-Api-Version": {"3"}}, total: -1, fallback: []string{}}
	items := []model.Object{}
	for start := 0; ; {
		page, err := scan.page(ctx, start, 100, z.HTTP)
		if err != nil {
			return Batch{}, err
		}
		if len(page) == 0 && scan.total > start {
			return Batch{}, errors.New("Zotero 条目扫描提前结束")
		}
		items = append(items, page...)
		start += len(page)
		if len(page) == 0 || scan.total >= 0 && start >= scan.total || scan.total < 0 && len(page) < 100 {
			break
		}
	}
	var server any
	if id := scan.headers.Get("Zotero-Server-ID"); id != "" {
		server = id
	}
	events, err := z.normalize(items, window, scan.headers.Get("Zotero-Server-ID"))
	return Batch{Events: events, Details: model.Object{
		"items_scanned": len(items), "server_id": server, "metadata_fallback_keys": scan.fallback,
		"method": "每日完整扫描现存条目；仅有创建/最后修改快照",
	}}, err
}

func (z *zoteroScan) page(ctx context.Context, start, limit int, client netclient.Client) ([]model.Object, error) {
	var page []model.Object
	headers, err := client.JSON(ctx, http.MethodGet, z.endpoint(), url.Values{
		"format": {"json"}, "limit": {strconv.Itoa(limit)}, "start": {strconv.Itoa(start)},
	}, z.headers, nil, &page)
	if errors.Is(err, netclient.StatusError(500)) {
		client.Attempts = 1
		if limit == 1 {
			item, err := z.bibliography(ctx, start, client)
			return []model.Object{item}, err
		}
		half := limit / 2
		first, err := z.page(ctx, start, half, client)
		if err != nil {
			return nil, err
		}
		second, err := z.page(ctx, start+half, limit-half, client)
		return append(first, second...), err
	}
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, errors.New("Zotero 返回了非条目列表")
	}
	if id := headers.Get("Zotero-Server-ID"); id != "" {
		z.headers.Set("Zotero-Server-ID", id)
	}
	if count := headers.Get("Total-Results"); count != "" {
		z.total, err = strconv.Atoi(count)
	}
	return page, err
}

func (z *zoteroScan) bibliography(ctx context.Context, start int, client netclient.Client) (model.Object, error) {
	_, raw, err := client.Request(ctx, http.MethodGet, z.endpoint(), url.Values{
		"format": {"keys"}, "limit": {"1"}, "start": {strconv.Itoa(start)},
	}, z.headers, nil)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return nil, errors.New("Zotero 无法导出条目元数据")
	}
	var papers []model.Object
	_, err = client.JSON(ctx, http.MethodGet, z.endpoint()+"/"+url.PathEscape(key), url.Values{
		"format": {"csljson"},
	}, z.headers, nil, &papers)
	if err != nil {
		return nil, err
	}
	if len(papers) != 1 {
		return nil, errors.New("Zotero 无法导出完整条目")
	}
	paper := papers[0]
	switch paper.Text("type") {
	case "article-journal", "article", "paper-conference", "book", "chapter", "thesis", "report", "manuscript", "webpage", "article-magazine", "article-newspaper":
	default:
		return nil, errors.New("Zotero 非论文条目 JSON 导出失败，不能跳过标注或笔记")
	}
	authors := []any{}
	for _, item := range paper.List("author") {
		author, _ := item.(map[string]any)
		fields := model.Object(author)
		if name := fields.Text("literal"); name != "" {
			authors = append(authors, map[string]any{"name": name})
		} else {
			authors = append(authors, map[string]any{"firstName": fields.Text("given"), "lastName": fields.Text("family")})
		}
	}
	z.fallback = append(z.fallback, key)
	return model.Object{"key": key, "data": map[string]any{
		"itemType": "bibliographic", "title": paper.Text("title"), "creators": authors,
		"metadata_format": "csljson", "csl_snapshot": paper,
	}}, nil
}

var htmlTag = regexp.MustCompile(`<[^>]+>`)

func (z Zotero) normalize(items []model.Object, window Window, server string) ([]model.Event, error) {
	if server == "" {
		server = "legacy-local"
	}
	namespace := server + ":" + z.Library
	libraryURI := z.Library
	if strings.HasPrefix(z.Library, "users/") {
		libraryURI = "library"
	}
	index := map[string]model.Object{}
	keys := []string{}
	for _, item := range items {
		key := item.Text("key")
		if key == "" {
			return nil, errors.New("Zotero 条目缺少稳定 key")
		}
		if _, exists := index[key]; !exists {
			keys = append(keys, key)
		}
		if data := item.Object("data"); data != nil {
			index[key] = data
		} else {
			index[key] = item
		}
	}
	events := []model.Event{}
	for _, key := range keys {
		data := index[key]
		kind := data.Text("itemType")
		if kind != "annotation" && kind != "note" || data.Bool("deleted") || identifier(data["deleted"]) == "1" {
			continue
		}
		paperKey := data.StringOr("parentItem", key)
		parent := index[paperKey]
		var attachment any
		if parent.Text("itemType") == "attachment" {
			attachment = paperKey
			paperKey = parent.StringOr("parentItem", paperKey)
			if paper, exists := index[paperKey]; exists {
				parent = paper
			}
		}
		title := parent.Text("title")
		if title == "" {
			title = "未关联论文（待确认）"
		}
		authors := []string{}
		for _, item := range parent.List("creators") {
			creator, _ := item.(map[string]any)
			fields := model.Object(creator)
			name := fields.Text("name")
			if name == "" {
				name = strings.TrimSpace(fields.Text("firstName") + " " + fields.Text("lastName"))
			}
			authors = append(authors, name)
		}
		description := strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(data.Text("note"), " ")))
		if kind == "annotation" {
			text := []string{}
			for _, field := range []string{"annotationText", "annotationComment"} {
				if value := data.Text(field); value != "" {
					text = append(text, value)
				}
			}
			description = strings.Join(text, "；")
		}
		rawCreated, rawModified := data.Text("dateAdded"), data.Text("dateModified")
		created, err := model.ParseTime(rawCreated, time.UTC)
		if err != nil {
			return nil, errors.New("Zotero 条目缺少有效 dateAdded")
		}
		modified := created
		if rawModified != "" {
			modified, err = model.ParseTime(rawModified, time.UTC)
			if err != nil {
				return nil, errors.New("Zotero 条目 dateModified 无效")
			}
		}
		for _, entry := range []struct {
			action, raw string
			at          time.Time
		}{{"created", rawCreated, created}, {"modified", rawModified, modified}} {
			if entry.action == "modified" && model.ISO(modified) == model.ISO(created) ||
				entry.at.After(window.Until) || !window.History && entry.at.Before(window.Since) {
				continue
			}
			event := model.NewEvent(model.StableID("zotero", namespace, key, entry.action), "zotero", key, kind+"_"+entry.action, title, entry.at)
			event.Description, event.Updated, event.Category, event.Raw = description, model.ISO(modified), model.Research, data
			event.URL = "zotero://select/" + libraryURI + "/items/" + paperKey
			if attachment != nil && kind == "annotation" {
				event.URL = "zotero://open-pdf/" + libraryURI + "/items/" + attachment.(string) + "?annotation=" + key
			}
			event.Metadata = model.Object{
				"paper_id": namespace + ":" + paperKey, "paper_key": paperKey, "authors": authors,
				"page": identifier(data["annotationPageLabel"]), "attachment_id": attachment,
				"original_timestamp": entry.raw, "date_added": rawCreated, "date_modified": data["dateModified"],
				"snapshot_only": true, "paper_metadata_format": parent.StringOr("metadata_format", "json"),
			}
			events = append(events, event)
		}
	}
	return events, nil
}
