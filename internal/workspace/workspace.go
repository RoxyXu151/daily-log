package workspace

import (
	"context"

	"daily-log/internal/model"
)

type Overview struct {
	Today        string
	TodaySummary model.JournalSummary
	Recent       []ActivitySummary
	LastRun      string
	NextRun      string
	Sources      []SourceStatus
	Outputs      []OutputStatus
	JobStatus    string
	JobError     string
}

type ActivitySummary struct {
	ID, Title, Source, Timestamp string
}

type OutputStatus struct {
	Name, Status, LastSuccess, ErrorMessage string
	Enabled                                 bool
	Summary                                 *OutputSummary
}

type OutputSummary struct {
	Files, Written, Protected, Skipped int
}

type SourceStatus struct {
	Name            string
	Enabled         bool
	Status          string
	LastSuccess     string
	Coverage        string
	ObservedThrough string
	ErrorMessage    string
	Attempt         *SourceAttempt
}

type SourceAttempt struct {
	RequestedSince, RequestedUntil, EffectiveSince string
	Returned, Changed, HistoryLimitDays            int
	HasCounts, HistoryGap                          bool
}

type Journal struct {
	View     model.JournalView
	Evidence []Evidence
	History  []model.JournalVersion
}

type Evidence struct {
	Category    string
	ID          string
	Source      string
	Title       string
	Kind        string
	Timestamp   string
	Description string
	URL         string
	EventType   string
	Reference   string
}

type Category struct {
	Name  string
	Short string
	Count int
}

type CategoryInput struct {
	Previous, Name, Short string
}

type Backend interface {
	SettingsBackend
	Journals(context.Context, model.BrowseQuery) (Page[model.JournalSummary], error)
	Activities(context.Context, model.BrowseQuery) (Page[Evidence], error)
	Activity(context.Context, string) (Evidence, error)
	SetEventCategory(context.Context, string, string) error
	Categories(context.Context) ([]Category, error)
	SaveCategories(context.Context, []CategoryInput) error
	Reading(context.Context, model.BrowseQuery) (Page[model.Session], error)
	Overview(context.Context) (Overview, error)
	Journal(context.Context, string) (Journal, error)
	Save(context.Context, string, string, model.JournalText, bool) (model.JournalView, error)
	Run(context.Context) error
	Recover(context.Context) error
	// Regenerate collects, then rewrites the last n days whether or not
	// they changed, and syncs: 整理最近一周.
	Regenerate(context.Context, int) error
	Export(context.Context, string) error
}

const PageSize = 40

type Page[T any] struct {
	Rows []T
	More bool
}
