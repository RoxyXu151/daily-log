package main

import (
	"slices"
	"strconv"
	"strings"

	"daily-log/internal/model"
	"daily-log/internal/workspace"

	"github.com/egoist/mygo/ui"
)

type categoryEdit struct {
	previous string
	name     string
	short    string
	count    int
}

// applyCategories is the one place the category list enters the screen: the
// display shorts travel with it, so no page can show a different name for
// the same category.
func (s *screen) applyCategories(items []workspace.Category) {
	s.categories = items
	setCategoryShorts(items)
}

func (s *screen) effectiveShort(item *categoryEdit) string {
	if short := strings.TrimSpace(item.short); short != "" {
		return short
	}
	return categoryShort(item.name)
}

func (s *screen) categoryNames() []string {
	names := make([]string, 0, len(s.categories))
	for _, category := range s.categories {
		if name := strings.TrimSpace(category.Name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return []string{model.Research, model.Course, model.Career, model.Life}
	}
	return names
}

func (s *screen) categoryChoices() []string {
	return append([]string{"全部类别"}, s.categoryNames()...)
}

func (s *screen) openCategories() {
	s.page = "categories"
	s.perform("读取分类设置…", func() (func(), error) {
		items, err := s.backend.Categories(s.ctx)
		if err != nil {
			return nil, err
		}
		return func() {
			s.applyCategories(items)
			s.resetCategoryEdits()
		}, nil
	})
}

func (s *screen) resetCategoryEdits() {
	s.categoryEdits = make([]categoryEdit, len(s.categories))
	for i, category := range s.categories {
		s.categoryEdits[i] = categoryEdit{previous: category.Name, name: category.Name, short: category.Short, count: category.Count}
	}
	s.categoryDraft = ""
	s.categoryDraftShort = ""
}

func (s *screen) categoriesScreen(c *ui.Context) {
	p := paletteFor(c.Theme())
	pageHeader(c, "CATEGORIES", "分类管理", "定义属于自己的活动分类；保存后会同步迁移历史活动、分钟数和日记总表列。")
	if s.busy && len(s.categoryEdits) == 0 {
		ui.Scroll(c.Key("categories-scroll")).Grow(1).MinHeight(0).Children(func() { settingsSkeleton(c) })
		return
	}
	ui.Scroll(c.Key("categories-scroll")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(spaceL).Children(func() {
			card(c.Key("category-list"), "分类列表", func() {
				ui.Text(c, "分类按这里的顺序显示在日记总表和筛选菜单中。删除已有分类时，相关活动会归入“未分类”。").
					FontSize(sizeBody).TextColor(p.muted).LineHeight(leadBody)
				if len(s.categoryEdits) == 0 {
					emptyState(c, iconCategories, "还没有分类", "添加第一个分类后即可给活动分类。")
				} else {
					// One surface divided by hairlines rather than a box
					// per row: the line between says they belong together,
					// the way the sidebar's rows do on the main page.
					// Indented to the inputs' own left edge (row padding, the
					// dot, the gap), so the label sits over the column it
					// names rather than over the panel.
					ui.Text(c, "分类名称").FontSize(sizeSmall).FontWeight(weightSemibold).
						LetterSpacing(trackingLabel).TextColor(p.faint).
						Margin(spaceS, 0, spaceXS, spaceM+16)
					edgeLit(ui.Column(c).Radius(radiusTile).Border(1, p.hairline), p).
						Dividers(1, p.hairline).Clip().Transition(surfaceFade).Children(func() {
						for i := range s.categoryEdits {
							s.categoryRow(c, i)
						}
					})
				}
				divider(c)
				ui.Row(c).Wrap().Gap(spaceS).AlignItems(ui.End).Children(func() {
					ui.Column(c).Grow(1).MinWidth(220).Children(func() {
						sectionLabel(c, "添加分类")
						ui.TextInput(c.Key("new-category"), &s.categoryDraft).Label("新分类名称").
							Placeholder("例如：论文写作、实验室、健身").Disabled(s.busy)
					})
					ui.Column(c).Shrink(0).Children(func() {
						sectionLabel(c, "简称（可选）")
						ui.TextInput(c.Key("new-category-short"), &s.categoryDraftShort).Label("新分类简称").
							Placeholder("日记总表中显示").Width(150).Disabled(s.busy)
					})
					if pillButton(c, "add-category", "添加", nil, false,
						s.busy || strings.TrimSpace(s.categoryDraft) == "").Clicked() {
						s.categoryEdits = append(s.categoryEdits, categoryEdit{
							name: strings.TrimSpace(s.categoryDraft), short: strings.TrimSpace(s.categoryDraftShort),
						})
						s.categoryDraft = ""
						s.categoryDraftShort = ""
					}
				})
				divider(c)
				actionRow(c, func() {
					if pillButton(c, "save-categories", "保存分类", iconSpark, true, s.busy).Clicked() {
						s.saveCategories()
					}
					if pillButton(c, "reset-categories", "撤销修改", nil, false, s.busy).Clicked() {
						s.resetCategoryEdits()
					}
				})
			})
		})
	})
}

func (s *screen) categoryRow(c *ui.Context, index int) {
	p := paletteFor(c.Theme())
	item := &s.categoryEdits[index]
	key := item.previous
	if key == "" {
		key = "new-" + item.name
	}
	// Rows join, leave and change place as one motion, at the page's own
	// assembly pace: a new row unfolds at the foot, a removed one folds
	// away, and swapping slides the pair in place.
	motion := disclosureMotion()
	motion.Animation = staggered(index)
	ui.Column(c.Key("category-" + key)).Padding(spaceM).Gap(spaceS).
		Transition(motion).Children(func() {
		ui.Row(c).Wrap().Gap(spaceM).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(spaceS).Grow(1).MinWidth(220).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(8, 8).Shrink(0).Radius(radiusPill).Background(categoryColor(item.name))
				ui.TextInput(c.Key("category-name-"+key), &item.name).
					Grow(1).MinWidth(0).Label("分类名称").Disabled(s.busy)
			})
			// 简称 is a field now, not a table in the code: every category
			// can carry the short label the journal sheet shows, and a new
			// one is no exception.
			ui.TextInput(c.Key("category-short-"+key), &item.short).Width(96).Shrink(0).
				Label("分类简称").Placeholder("简称").Disabled(s.busy)
			// The count keeps a slot of its own: a pill that hugs its words
			// would push 上移/下移/删除 out of column from row to row.
			ui.Box(c).Width(104).Shrink(0).Children(func() {
				statusPill(c, categoryCountLabel(item.count), toneNeutral, "category-count-"+key)
			})
			// The row's own controls travel together as one unit when the
			// row wraps, each hugging its words.
			ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Shrink(0).Children(func() {
				if pillButton(c, "category-up-"+key, "上移", nil, false,
					s.busy || index == 0).Clicked() {
					s.categoryEdits[index-1], s.categoryEdits[index] = s.categoryEdits[index], s.categoryEdits[index-1]
				}
				if pillButton(c, "category-down-"+key, "下移", nil, false,
					s.busy || index == len(s.categoryEdits)-1).Clicked() {
					s.categoryEdits[index+1], s.categoryEdits[index] = s.categoryEdits[index], s.categoryEdits[index+1]
				}
				if pillButton(c, "category-remove-"+key, "删除", nil, false,
					s.busy || item.previous == model.Uncategorized).Clicked() {
					s.categoryEdits = slices.Delete(s.categoryEdits, index, index+1)
				}
			})
		})
		if item.previous == model.Uncategorized {
			ui.Text(c, "“未分类”是删除分类时的安全归集位置，不能移除。").
				FontSize(sizeMeta).TextColor(p.faint)
		} else if item.count > 0 && item.name != item.previous {
			ui.Textf(c, "保存后，%d 条已有活动会迁移到新名称。", item.count).
				FontSize(sizeMeta).TextColor(p.muted).FontFeatures("tnum")
		} else if short := s.effectiveShort(item); short != item.name {
			// The journal sheet says 科研 where this page says 科研学习与论文:
			// naming the short form here keeps the two pages from reading
			// as two different categories.
			ui.Textf(c, "在日记总表中显示为「%s」。", short).
				FontSize(sizeMeta).TextColor(p.faint)
		}
	})
}

func categoryCountLabel(count int) string {
	if count == 0 {
		return "暂无活动"
	}
	return strconv.Itoa(count) + " 条活动"
}

func (s *screen) saveCategories() {
	items := make([]workspace.CategoryInput, 0, len(s.categoryEdits))
	for _, item := range s.categoryEdits {
		items = append(items, workspace.CategoryInput{Previous: item.previous, Name: item.name, Short: item.short})
	}
	s.perform("正在保存分类…", func() (func(), error) {
		if err := s.backend.SaveCategories(s.ctx, items); err != nil {
			return nil, err
		}
		categories, err := s.backend.Categories(s.ctx)
		if err != nil {
			return nil, err
		}
		return func() {
			s.applyCategories(categories)
			s.resetCategoryEdits()
			s.notify("分类已保存，日记总表和活动筛选已更新")
		}, nil
	})
}
