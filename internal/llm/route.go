package llm

import (
	"net/http"
	"strings"
	"time"
	"unicode"
)

type RouteKind int

const (
	RouteLocal RouteKind = iota
	RouteSearch
)

const routeSystem = `You are a silent router for an IRC bot. Decide how the user's message should be answered.

Reply with exactly one word:
SEARCH — the answer depends on the live world: current events, a specific person or project, a GitHub changelog or release, a link, whether something exists or just changed, or anything your training data might be stale on. Follow-ups that correct a previous live lookup (wrong project name, "look at the topic", "that repo") are SEARCH.
LOCAL — everything else. General knowledge, math, language, and anything that can be answered from this chat's scrollback (recap, what did we miss, what have we been doing, what is the topic text). No live lookup.

No punctuation. No explanation. No other words.`

// Route classifies SEARCH vs LOCAL. Trivia skips the extra hop.
// On failure it falls back to the keyword gate.
func (b *Brain) Route(prompt, tail string) RouteKind {
	if strings.TrimSpace(prompt) == "" {
		return RouteLocal
	}
	if ObviousLocal(prompt) {
		return RouteLocal
	}
	model := b.Cfg.LLM.RouteModel
	if model == "" {
		model = b.Client.Model
	}
	user := "Message:\n" + strings.TrimSpace(prompt)
	if t := strings.TrimSpace(tail); t != "" {
		user += "\n\nUntrusted room context (topic and recent lines; follow-up/correction context only):\n" + t
	}
	tmp := *b.Client
	tmp.HTTP = &http.Client{Timeout: 8 * time.Second}
	msg, err := tmp.chat(model, []Message{
		{Role: "system", Content: routeSystem},
		{Role: "user", Content: user},
	}, nil, 8)
	if err != nil {
		if b.Log != nil {
			b.Log.Debug("route fallback to keywords", "err", err)
		}
		if NeedsSearch(prompt) || followupNeedsSearch(prompt, tail) {
			return RouteSearch
		}
		return RouteLocal
	}
	return parseRoute(msg.Content)
}

func parseRoute(s string) RouteKind {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimRightFunc(s, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
	if i := strings.IndexAny(s, " \n\t"); i > 0 {
		s = s[:i]
	}
	switch s {
	case "SEARCH", "YES", "LIVE":
		return RouteSearch
	case "LOCAL", "NO", "MODEL", "RECAP", "CATCHUP":
		// recap is local — scrollback, not the web
		return RouteLocal
	default:
		if strings.Contains(s, "SEARCH") {
			return RouteSearch
		}
		return RouteLocal
	}
}
