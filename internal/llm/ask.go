package llm

import (
	"strings"
	"unicode"
)

const (
	DestChannel = "channel"
	DestDM      = "dm"
	DestCatchup = "catchup"
)

// IsCatchup reports whether the prompt is a "what did I miss" / room-recap request.
func IsCatchup(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimRightFunc(s, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
	switch s {
	case "catch me up", "catchup", "catch-up", "catch up",
		"summary", "summarize", "recap", "backfill",
		"what did i miss", "what did we miss", "what happened",
		"fill me in", "bring me up to speed":
		return true
	}
	for _, p := range []string{
		"catch me up", "catch us up", "catch them up",
		"what did i miss", "what did we miss",
		"what have we been doing", "what we've been doing", "what we have been doing",
		"what have we done", "what we've done",
		"summarize what we", "recap what we",
		"summarize the channel", "summarize this channel", "summarize the room", "summarize this room",
		"recap the channel", "recap this channel", "recap the room",
		"bring me up to speed", "bring us up to speed",
		"fill me in", "fill us in",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	// "catch chrisk up" — one token between catch and up
	if i := strings.Index(s, "catch "); i >= 0 {
		rest := s[i+len("catch "):]
		if j := strings.Index(rest, " up"); j > 0 && !strings.Contains(rest[:j], " ") {
			return true
		}
	}
	if strings.Contains(s, "bring ") && strings.Contains(s, " up to speed") {
		return true
	}
	return false
}

// NeedsSearch is a cheap gate so trivia/math stay on the fast chat path.
// Live search (web + X) only when the wording smells like "now" or "find this".
func NeedsSearch(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || IsCatchup(s) {
		return false
	}
	for _, p := range []string{
		"today", "tonight", "yesterday", "this week", "this month",
		"latest", "current", "right now", "just released", "just went",
		"hours ago", "went live", "how new", "how old is this",
		"look up", "look it up", "search", "find the", "find me",
		"got a link", "any link", "the url", "the site",
		"homepage", "docs for", "source code", "open source",
		"who runs", "who owns", "who wrote", "who makes",
		"is it public", "is it live", "still private",
		"on x", "on twitter", "tweet",
		"changelog", "change log", "github", "latest release",
		"look at the topic", "channel topic", "that repo", "that project",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	// "is X on Y" / "where is X hosted" — discovery, not capitals
	if strings.Contains(s, " where ") || strings.HasPrefix(s, "where ") {
		if strings.Contains(s, "host") || strings.Contains(s, "live") || strings.Contains(s, "code") {
			return true
		}
	}
	if (strings.Contains(s, " is it ") || strings.Contains(s, " is this ") || strings.Contains(s, " is the ")) &&
		(strings.Contains(s, " on ") || strings.Contains(s, " at ") || strings.Contains(s, " public")) {
		return true
	}
	return false
}

// ObviousLocal is conservative trivia/math that must never wait on the router.
// Keep this narrow — "what is X" is often a live lookup, not a definition.
func ObviousLocal(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || IsCatchup(s) {
		return false
	}
	for _, p := range []string{
		"capital of", "capital city of",
		"square root", "cube root",
		"how do you spell", "how to spell",
		"synonym of", "antonym of", "plural of", "past tense of",
		"in celsius", "in fahrenheit", "in kilometers", "in miles",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return isSimpleMath(s)
}

func isSimpleMath(s string) bool {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	if compact == "" {
		return false
	}
	hasDigit := false
	for _, r := range compact {
		switch {
		case unicode.IsDigit(r) || r == '.':
			hasDigit = true
		case strings.ContainsRune("+-*/^()%=,", r):
		default:
			return false
		}
	}
	return hasDigit
}

func wantsRepoLookup(prompt string, hist []HistoryLine) bool {
	if repoQuestion(prompt) {
		return true
	}
	if looksLikeCorrection(prompt) || mentionsTopic(prompt) {
		for _, h := range hist {
			if repoQuestion(h.Text) {
				return true
			}
		}
		return mentionsTopic(prompt)
	}
	return false
}

func repoQuestion(s string) bool {
	s = strings.ToLower(s)
	for _, p := range []string{
		"changelog", "change log", "github", "latest release",
		"this irc", "this server", "this project",
		"that repo", "that project", "latest version",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func looksLikeCorrection(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return false
	}
	for _, p := range []string{
		" not ", ", not", "not ",
		"oof", "wrong", "i meant", "i mean",
		"got it", "it's ", "its ",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func mentionsTopic(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "topic") || strings.Contains(s, "that repo") || strings.Contains(s, "that url")
}

func followupNeedsSearch(prompt, context string) bool {
	if !looksLikeCorrection(prompt) && !mentionsTopic(prompt) {
		return false
	}
	c := strings.ToLower(context)
	return strings.Contains(c, "github") || strings.Contains(c, "changelog") || strings.Contains(c, "http")
}
