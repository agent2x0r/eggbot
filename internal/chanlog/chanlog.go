// Package chanlog is durable public-channel scrollback for !history.
package chanlog

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"eggbot/internal/irccase"
	"eggbot/internal/store"
)

const (
	maxText    = 400
	maxReturn  = 40
	defaultWin = 14 * 24 * time.Hour
)

type Line struct {
	Nick string
	Text string
	At   time.Time
}

type Module struct {
	st *store.Store
}

func New(st *store.Store) *Module { return &Module{st: st} }

func (m *Module) Add(channel, nick, text string) error {
	if m == nil || m.st == nil {
		return nil
	}
	channel = irccase.Fold(channel)
	text = strings.TrimSpace(text)
	if channel == "" || text == "" {
		return nil
	}
	if len(text) > maxText {
		text = text[:maxText]
	}
	_, err := m.st.DB.Exec(
		`INSERT INTO chanlog (channel, nick, text, at) VALUES (?, ?, ?, ?)`,
		channel, nick, text, store.Now(),
	)
	return err
}

func (m *Module) Search(channel, query string, now time.Time) ([]Line, error) {
	if m == nil || m.st == nil {
		return nil, nil
	}
	channel = irccase.Fold(channel)
	from, to, words := ParseQuery(query, now)
	rows, err := m.st.DB.Query(
		`SELECT nick, text, at FROM chanlog WHERE channel = ? AND at >= ? AND at <= ? ORDER BY at DESC LIMIT 200`,
		channel, from.Unix(), to.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Line
	for rows.Next() {
		var n, t string
		var ts int64
		if err := rows.Scan(&n, &t, &ts); err != nil {
			return nil, err
		}
		if !matchWords(t, words) {
			continue
		}
		out = append(out, Line{Nick: n, Text: t, At: time.Unix(ts, 0)})
		if len(out) >= maxReturn {
			break
		}
	}
	reverseLines(out)
	return out, rows.Err()
}

func Format(lines []Line) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.At.UTC().Format("2006-01-02 15:04"))
		b.WriteByte(' ')
		b.WriteByte('<')
		b.WriteString(l.Nick)
		b.WriteString("> ")
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

var (
	reLast = regexp.MustCompile(`(?i)\blast\s+(\d+)\s+(minutes?|hours?|days?|weeks?|months?)\b`)
	reAgo  = regexp.MustCompile(`(?i)\b(\d+)\s+(minutes?|hours?|days?|weeks?|months?)\s+ago\b`)
	rePast = regexp.MustCompile(`(?i)\bpast\s+(\d+)\s+(minutes?|hours?|days?|weeks?|months?)\b`)
)

func ParseQuery(query string, now time.Time) (from, to time.Time, words []string) {
	q := strings.TrimSpace(query)
	to = now
	from = now.Add(-defaultWin)
	if m := reLast.FindStringSubmatch(q); len(m) == 3 {
		from = now.Add(-durationN(m[1], m[2]))
		q = reLast.ReplaceAllString(q, " ")
	} else if m := rePast.FindStringSubmatch(q); len(m) == 3 {
		from = now.Add(-durationN(m[1], m[2]))
		q = rePast.ReplaceAllString(q, " ")
	} else if m := reAgo.FindStringSubmatch(q); len(m) == 3 {
		d := durationN(m[1], m[2])
		center := now.Add(-d)
		slack := d / 2
		if slack < 24*time.Hour {
			slack = 24 * time.Hour
		}
		from = center.Add(-slack)
		to = center.Add(slack)
		if to.After(now) {
			to = now
		}
		q = reAgo.ReplaceAllString(q, " ")
	}
	for _, w := range strings.Fields(strings.ToLower(q)) {
		w = strings.TrimFunc(w, func(r rune) bool { return unicode.IsPunct(r) })
		if len(w) < 3 {
			continue
		}
		switch w {
		case "discussed", "discussing", "topic", "about", "the", "and", "for":
			continue
		}
		words = append(words, w)
	}
	return from, to, words
}

func durationN(num, unit string) time.Duration {
	n, _ := strconv.Atoi(num)
	if n <= 0 {
		n = 1
	}
	switch {
	case strings.HasPrefix(strings.ToLower(unit), "minute"):
		return time.Duration(n) * time.Minute
	case strings.HasPrefix(strings.ToLower(unit), "hour"):
		return time.Duration(n) * time.Hour
	case strings.HasPrefix(strings.ToLower(unit), "day"):
		return time.Duration(n) * 24 * time.Hour
	case strings.HasPrefix(strings.ToLower(unit), "week"):
		return time.Duration(n) * 7 * 24 * time.Hour
	default:
		return time.Duration(n) * 30 * 24 * time.Hour
	}
}

func matchWords(text string, words []string) bool {
	if len(words) == 0 {
		return true
	}
	low := strings.ToLower(text)
	for _, w := range words {
		if !strings.Contains(low, w) {
			return false
		}
	}
	return true
}

func reverseLines(s []Line) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
