// Package chanlog is durable public-channel scrollback for !history.
package chanlog

import (
	"regexp"
	"sort"
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
		`SELECT nick, text, at FROM chanlog WHERE channel = ? AND at >= ? AND at <= ? ORDER BY at DESC`,
		channel, from.Unix(), to.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type scored struct {
		line  Line
		score int
	}
	var hit []scored
	qfold := strings.ToLower(strings.TrimSpace(query))
	for rows.Next() {
		var n, t string
		var ts int64
		if err := rows.Scan(&n, &t, &ts); err != nil {
			return nil, err
		}
		low := strings.ToLower(strings.TrimSpace(t))
		if strings.HasPrefix(low, "!history") {
			continue
		}
		if qfold != "" && (low == qfold || (len(qfold) >= 24 && strings.Contains(low, qfold))) {
			continue
		}
		score := matchScore(t, words)
		if score <= 0 {
			continue
		}
		hit = append(hit, scored{Line{Nick: n, Text: t, At: time.Unix(ts, 0)}, score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(hit, func(i, j int) bool {
		if hit[i].score != hit[j].score {
			return hit[i].score > hit[j].score
		}
		return hit[i].line.At.After(hit[j].line.At)
	})
	if len(hit) > maxReturn {
		hit = hit[:maxReturn]
	}
	out := make([]Line, len(hit))
	for i, h := range hit {
		out[i] = h.line
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
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
	seen := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(q)) {
		w = strings.TrimFunc(w, func(r rune) bool { return unicode.IsPunct(r) })
		if len(w) < 3 || stopWord[w] || seen[w] {
			continue
		}
		seen[w] = true
		words = append(words, w)
	}
	return from, to, words
}

var stopWord = map[string]bool{
	"about": true, "and": true, "the": true, "for": true, "discussed": true, "discussing": true, "topic": true,
	"you": true, "your": true, "yours": true, "remember": true, "recall": true, "said": true, "say": true,
	"had": true, "has": true, "have": true, "how": true, "much": true, "many": true, "what": true, "when": true,
	"where": true, "which": true, "who": true, "whom": true, "this": true, "that": true, "these": true, "those": true,
	"talked": true, "talking": true, "talk": true, "other": true, "day": true, "days": true, "just": true, "like": true,
	"was": true, "were": true, "been": true, "being": true, "will": true, "would": true, "could": true, "should": true,
	"want": true, "know": true, "think": true, "well": true, "yeah": true, "really": true, "some": true, "more": true,
	"from": true, "with": true, "they": true, "them": true, "then": true, "than": true, "also": true, "into": true,
	"over": true, "only": true, "even": true, "back": true, "here": true, "there": true, "come": true, "came": true,
	"very": true, "did": true, "does": true, "doing": true, "got": true, "get": true, "any": true, "can": true,
	"our": true, "out": true, "all": true, "but": true, "not": true, "are": true, "its": true, "it's": true,
}

var wordExpand = map[string][]string{
	"storage": {"storage", "drive", "drives", "disk", "disks", "ssd", "nvme", "hdd", "tb", "gb", "array"},
	"drive":   {"drive", "drives", "disk", "ssd", "nvme", "hdd", "tb", "array"},
	"memory":  {"memory", "ram", "ddr", "gb"},
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

func matchScore(text string, words []string) int {
	if len(words) == 0 {
		return 1
	}
	low := strings.ToLower(text)
	n := 0
	for _, w := range words {
		if wordHits(low, w) {
			n++
		}
	}
	return n
}

func wordHits(low, w string) bool {
	alts := wordExpand[w]
	if len(alts) == 0 {
		alts = []string{w}
	}
	for _, a := range alts {
		if strings.Contains(low, a) {
			return true
		}
	}
	return false
}
