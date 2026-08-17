// Package github is a bounded, read-only GitHub repository lookup for IRC answers.
package github

import (
	"net/url"
	"strings"
	"unicode"
)

var skipOwners = map[string]struct{}{
	"about": {}, "features": {}, "marketplace": {}, "orgs": {},
	"search": {}, "settings": {}, "topics": {}, "login": {},
	"signup": {}, "notifications": {}, "explore": {}, "apps": {},
}

// Parse extracts owner/repo from a GitHub URL or "owner/repo" spec.
func Parse(s string) (owner, repo string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	s = strings.Trim(s, "<>")
	s = strings.TrimRight(s, ".,);!?'\"")
	if owner, repo, ok = parseURL(s); ok {
		return owner, repo, true
	}
	if strings.Contains(s, "://") {
		return "", "", false
	}
	if i := strings.Index(strings.ToLower(s), "github.com/"); i >= 0 {
		return parseURL("https://" + s[i:])
	}
	if owner, repo, ok := splitOwnerRepo(s); ok {
		return owner, repo, true
	}
	return "", "", false
}

// Find returns the first GitHub owner/repo in texts, searching each string
// left-to-right and texts in the given order.
func Find(texts ...string) (owner, repo string, ok bool) {
	for _, text := range texts {
		if owner, repo, ok = findOne(text); ok {
			return owner, repo, true
		}
	}
	return "", "", false
}

func findOne(text string) (owner, repo string, ok bool) {
	lower := strings.ToLower(text)
	for {
		i := strings.Index(lower, "github.com/")
		if i < 0 {
			break
		}
		start := i
		if start > 0 {
			r := rune(lower[start-1])
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				lower = lower[i+1:]
				text = text[i+1:]
				continue
			}
		}
		chunk := text[start:]
		end := len(chunk)
		for j, r := range chunk {
			if unicode.IsSpace(r) || r == '>' || r == ')' || r == '"' || r == '\'' {
				end = j
				break
			}
		}
		if owner, repo, ok = Parse(chunk[:end]); ok {
			return owner, repo, true
		}
		if i+1 >= len(lower) {
			break
		}
		lower = lower[i+1:]
		text = text[i+1:]
	}
	return "", "", false
}

func parseURL(s string) (owner, repo string, ok bool) {
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", "", false
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	if host != "github.com" {
		return "", "", false
	}
	path := strings.Trim(u.Path, "/")
	return splitOwnerRepo(path)
}

func splitOwnerRepo(s string) (owner, repo string, ok bool) {
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner = parts[0]
	repo = parts[1]
	repo = strings.TrimSuffix(repo, ".git")
	if !validName(owner) || !validName(repo) {
		return "", "", false
	}
	if _, skip := skipOwners[strings.ToLower(owner)]; skip {
		return "", "", false
	}
	return owner, repo, true
}

func validName(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 100 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-' || r == '_':
		case r == '.' && i > 0:
		default:
			return false
		}
	}
	return true
}

func Canonical(owner, repo string) string {
	return owner + "/" + repo
}

func PageURL(owner, repo string) string {
	return "https://github.com/" + owner + "/" + repo
}
