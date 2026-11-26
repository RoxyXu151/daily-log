package source

import (
	"slices"

	"daily-log/internal/model"
)

// Reconciler marks only events for which a collector observed a cancellation
// or fully read the original scope and no longer returned the event.
func (b Batch) Reconciler(name string) func(model.Event) (model.Event, bool) {
	switch name {
	case "codex":
		scopes := b.Details.List("completed_scopes")
		if len(scopes) == 0 {
			return nil
		}
		incoming := make(map[string]bool, len(b.Events))
		for _, event := range b.Events {
			incoming[event.ID] = true
		}
		return func(event model.Event) (model.Event, bool) {
			if incoming[event.ID] || event.Metadata.Bool("cancelled") {
				return event, false
			}
			scope := event.Metadata.List("scope")
			for _, item := range scopes {
				completed, _ := item.([]any)
				if len(scope) == 4 && slices.EqualFunc(scope, completed, sameString) {
					event.Metadata["cancelled"] = true
					return event, true
				}
			}
			return event, false
		}
	case "calendar":
		cancellations := b.Details.List("cancelled")
		if len(cancellations) == 0 {
			return nil
		}
		return func(event model.Event) (model.Event, bool) {
			if event.Metadata.Bool("cancelled") {
				return event, false
			}
			for _, item := range cancellations {
				object, _ := item.(map[string]any)
				fields := model.Object(object)
				if fields.Text("id") == event.ObjectID && fields.Text("calendar_id") == event.Metadata.Text("calendar_id") {
					event.Kind = "calendar_cancelled"
					event.Metadata["cancelled"] = true
					return event, true
				}
			}
			return event, false
		}
	default:
		return nil
	}
}

func sameString(a, b any) bool {
	left, ok := a.(string)
	right, otherOK := b.(string)
	return ok && otherOK && left == right
}
