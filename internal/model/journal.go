package model

import "errors"

var ErrJournalConflict = errors.New("这篇日记已有更新。当前输入已保留，请查看已保存版本后再处理")
var ErrReviewUnavailable = errors.New("这条日记建议已复核或不可用")

type JournalText struct {
	Morning   string `json:"morning"`
	Afternoon string `json:"afternoon"`
	Evening   string `json:"evening"`
	Note      string `json:"note"`
}

func JournalTextFrom(payload Object) JournalText {
	return JournalText{payload.Text("上午"), payload.Text("下午"), payload.Text("晚上"), payload.Text("manual")}
}

func (t JournalText) Apply(payload Object) {
	payload["上午"], payload["下午"], payload["晚上"], payload["manual"] = t.Morning, t.Afternoon, t.Evening, t.Note
}

type JournalSummary struct {
	Day               string            `json:"day"`
	Status            string            `json:"status"`
	Weekday           string            `json:"weekday"`
	Summary           string            `json:"summary"`
	Morning           string            `json:"morning"`
	Afternoon         string            `json:"afternoon"`
	Evening           string            `json:"evening"`
	Categories        []JournalCategory `json:"categories"`
	CategoryDigests   []CategoryDigest  `json:"category_digests"`
	Topics            []JournalTopic    `json:"topics"`
	Sources           []string          `json:"sources"`
	Completed         int               `json:"completed"`
	EventCount        int               `json:"event_count"`
	ConversationCount int               `json:"conversation_count"`
	TopicCount        int               `json:"topic_count"`
	ActiveStart       string            `json:"active_start"`
	ActiveEnd         string            `json:"active_end"`
}

type JournalCategory struct {
	Name    string   `json:"name"`
	Count   int      `json:"count"`
	Minutes *float64 `json:"minutes,omitempty"`
}

type CategoryDigest struct {
	Category  string `json:"category"`
	Summary   string `json:"summary"`
	ItemCount int    `json:"item_count"`
}

type JournalTopic struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Period        string   `json:"period"`
	Category      string   `json:"category"`
	Source        string   `json:"source"`
	Start         string   `json:"start"`
	End           string   `json:"end"`
	EventCount    int      `json:"event_count"`
	Conversations int      `json:"conversations"`
	Completed     int      `json:"completed"`
	Evidence      []string `json:"evidence"`
}

type DailyDigest struct {
	Summary           string           `json:"summary"`
	CategorySummaries []CategoryDigest `json:"category_summaries"`
	WorkItems         []JournalTopic   `json:"work_items"`
}

type JournalView struct {
	Record   Record `json:"record"`
	Revision string `json:"revision"`
}

type Suggestion struct {
	ID        int64  `json:"id"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
	Payload   Object `json:"payload"`
}

type ReviewDay struct {
	Day, Latest string
	Count       int
}

type BrowseQuery struct {
	Day, Source, Category, Keyword string
	Status                         string
	Offset                         int
	Ascending                      bool
}

type JournalVersion struct {
	ID         int64       `json:"id"`
	Status     string      `json:"status"`
	Action     string      `json:"action"`
	ReplacedAt string      `json:"replaced_at"`
	Text       JournalText `json:"text"`
}
