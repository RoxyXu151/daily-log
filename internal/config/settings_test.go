package config

import "testing"

func TestSingleEffectiveModelMigratesLegacyTaskSelections(t *testing.T) {
	settings := Settings{
		Profiles: []Profile{
			{ID: "journal-model", Model: "shared-model", Enabled: true},
			{ID: "codex-model", Model: "ignored-model", Enabled: true},
		},
		Selections: map[string]string{
			JournalModel: "journal-model",
			CodexModel:   "codex-model",
		},
	}
	if got := settings.EffectiveModelID(); got != "journal-model" {
		t.Fatalf("effective model = %q", got)
	}
	cfg := Config{Settings: settings}
	journal, _ := cfg.Model(JournalModel)
	codex, _ := cfg.Model(CodexModel)
	if journal.Text("model") != "shared-model" || journal.Text("model") != codex.Text("model") {
		t.Fatal("legacy task selections did not converge on one model")
	}
}

func TestFirstEnabledModelBecomesDefault(t *testing.T) {
	var settings Settings
	settings.ReplaceProfile(Profile{ID: "first", Enabled: true})
	if settings.DefaultModel != "first" || settings.EffectiveModelID() != "first" {
		t.Fatalf("first enabled model was not selected: %+v", settings)
	}
	settings.ReplaceProfile(Profile{ID: "first", Enabled: false})
	if settings.EffectiveModelID() != "" {
		t.Fatalf("a disabled default should leave no model, not a rules fallback: %+v", settings)
	}
}
