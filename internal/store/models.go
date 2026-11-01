package store

import "strings"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// History kinds.
const (
	KindSearch = "search"
	KindOpen   = "open"
)

// HistoryEntry is one search, or one result opened.
type HistoryEntry struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Query     string `json:"query"`
	Category  string `json:"category"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Results   int    `json:"results"`
	CreatedAt string `json:"created_at"`
}

// Site rule actions, strongest first.
const (
	RulePin   = "pin"   // always on top
	RuleBoost = "boost" // higher up
	RuleLower = "lower" // further down
	RuleBlock = "block" // never shown
)

// ValidRule reports whether a is a site rule action.
func ValidRule(a string) bool {
	return a == RulePin || a == RuleBoost || a == RuleLower || a == RuleBlock
}

// SiteRule is a person's rule for one site and its subdomains.
type SiteRule struct {
	ID        int64  `json:"id"`
	Domain    string `json:"domain"`
	Action    string `json:"action"`
	Note      string `json:"note"`
	Hits      int    `json:"hits"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Bang is a person's own shortcut: "!trigger words" opens URL with {q}.
type Bang struct {
	ID        int64  `json:"id"`
	Trigger   string `json:"trigger"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Uses      int    `json:"uses"`
	CreatedAt string `json:"created_at"`
}

// Save is a result sent to another Pocket app.
type Save struct {
	ID        int64  `json:"id"`
	URL       string `json:"url"`
	App       string `json:"app"`
	Title     string `json:"title"`
	ItemURL   string `json:"item_url"`
	CreatedAt string `json:"created_at"`
}

// AppConn is how one person reaches one sibling Pocket app: through Pocket
// Core (nothing to paste), directly with their own address and token, or not
// at all. Mode "" = the default (Core when there is one).
type AppConn struct {
	Mode   string `json:"mode,omitempty"` // core | direct | off
	URL    string `json:"url,omitempty"`
	Token  string `json:"token,omitempty"`
	Public string `json:"public,omitempty"` // the app's address in a browser, for links
	Search *bool  `json:"search,omitempty"` // include it in "From your apps" (nil = yes)
}

// Settings are the box-wide values plus one person's preferences.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`

	// Searching.
	SafeSearch      int    `json:"safe_search"`      // 0 off | 1 moderate | 2 strict
	Language        string `json:"language"`         // "auto" or a code like "en", "sr", "de-DE"
	DefaultCategory string `json:"default_category"` // general | images | videos | news
	NewTab          bool   `json:"new_tab"`          // open results in a new tab
	Suggestions     bool   `json:"suggestions"`      // suggest while typing
	ReaderImages    bool   `json:"reader_images"`    // pictures in the reading view (through this server)
	// Engines: names switched off that are on by default, and names switched
	// on that are off by default (SearXNG's own defaults otherwise).
	EnginesOff []string `json:"engines_off"`
	EnginesOn  []string `json:"engines_on"`

	// History.
	History     bool `json:"history"`      // remember searches
	HistoryDays int  `json:"history_days"` // forget after this many days (0 = keep)

	// Your Pocket apps in the results.
	AppsInResults bool               `json:"apps_in_results"`
	Apps          map[string]AppConn `json:"apps"`

	// AI answers above the results.
	AIAnswers     string `json:"ai_answers"` // off | ask (a button) | auto
	AIReadPages   bool   `json:"ai_read_pages"`
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }

// Lines splits a multi-line setting into its trimmed, non-empty lines.
func Lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
