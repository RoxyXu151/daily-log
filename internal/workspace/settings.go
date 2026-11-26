package workspace

import "context"

type Field struct {
	Key, Label, Kind, Value string
	Configured              bool
	ReadOnly                bool
}

type Connection struct {
	ID, Label, Description string
	Enabled, Removed       bool
	// Added tells whether the source sits in the user's list; catalog
	// sources wait out of the list until an "添加来源" saves one.
	Added bool
	// Integration names the standard access method: api, local-api, file,
	// oauth, or command.
	Integration string
	Fields      []Field
}

type ConnectionInput struct {
	ID      string
	Enabled bool
	Values  map[string]string
	Secrets map[string]string // Keys are field IDs. An omitted key leaves the secret in place.
}

type Profile struct {
	ID, Name, BaseURL, Model string
	Enabled, Anonymous       bool
	HasToken                 bool
}

type ProfileInput struct {
	Profile
	Token *string // nil leaves the stored token in place.
}

type ModelSettings struct {
	Profiles                []Profile
	Default                 string
	LegacyName              string
	StopOnJournalModelError bool
}

type SettingsBackend interface {
	Schedule(context.Context) (ScheduleSettings, error)
	SaveSchedule(context.Context, ScheduleInput) error
	InstallSchedule(context.Context) error
	Connections(context.Context) ([]Connection, error)
	SaveConnection(context.Context, ConnectionInput) error
	RemoveConnection(context.Context, string) error
	TestConnection(context.Context, ConnectionInput) (string, error)
	Models(context.Context) (ModelSettings, error)
	SaveModel(context.Context, ProfileInput) (string, error)
	TestModel(context.Context, ProfileInput) error
	// ListModels asks the service for the models it serves, so 模型名称 can
	// be chosen instead of typed.
	ListModels(context.Context, ProfileInput) ([]string, error)
	RemoveModel(context.Context, string) error
	SetDefaultModel(context.Context, string) error
	SetJournalModelFailureMode(context.Context, bool) error
}

type ScheduleInput struct {
	Clock        string
	LookbackDays int
	Excluded     []string
}

type ScheduleSettings struct {
	ScheduleInput
	Next          string
	LastInstalled string
	AppliedClock  string
	EventTypes    []EventType
}

type EventType struct {
	ID, Label string
	Today     int
}
