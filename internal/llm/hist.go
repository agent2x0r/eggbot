package llm

import (
	"encoding/json"
	"time"

	"eggbot/internal/irccase"
)

// ChatHistMaxAge is how recent a disk snapshot must be to refill RAM after a restart.
const ChatHistMaxAge = 5 * time.Minute

type histSnap struct {
	SavedAt  int64                     `json:"saved_at"`
	Channels map[string][]histSnapLine `json:"channels"`
}

type histSnapLine struct {
	Nick string `json:"nick"`
	Text string `json:"text"`
	At   int64  `json:"at"`
}

func (b *Brain) DumpHist(now time.Time) ([]byte, error) {
	if b == nil {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	snap := histSnap{SavedAt: now.Unix(), Channels: map[string][]histSnapLine{}}
	for ch, lines := range b.hist {
		if len(lines) == 0 {
			continue
		}
		out := make([]histSnapLine, 0, len(lines))
		for _, h := range lines {
			at := h.At.Unix()
			if at <= 0 {
				at = now.Unix()
			}
			out = append(out, histSnapLine{Nick: h.Nick, Text: h.Text, At: at})
		}
		snap.Channels[ch] = out
	}
	if len(snap.Channels) == 0 {
		return nil, nil
	}
	return json.Marshal(snap)
}

// RestoreHist loads a DumpHist snapshot if it is newer than ChatHistMaxAge.
// Returns the number of lines restored, or 0 if the blob was missing/stale.
func (b *Brain) RestoreHist(raw []byte, now time.Time) (int, error) {
	if b == nil || len(raw) == 0 {
		return 0, nil
	}
	var snap histSnap
	if err := json.Unmarshal(raw, &snap); err != nil {
		return 0, err
	}
	if snap.SavedAt <= 0 || now.Sub(time.Unix(snap.SavedAt, 0)) > ChatHistMaxAge {
		return 0, nil
	}
	max := 40
	if b.Cfg != nil && b.Cfg.LLM.Limits.HistoryLines > 0 {
		max = b.Cfg.LLM.Limits.HistoryLines
	}
	n := 0
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch, lines := range snap.Channels {
		if len(lines) == 0 {
			continue
		}
		if len(lines) > max {
			lines = lines[len(lines)-max:]
		}
		dst := make([]HistoryLine, 0, len(lines))
		for _, h := range lines {
			if h.Text == "" {
				continue
			}
			dst = append(dst, HistoryLine{
				Nick: h.Nick,
				Text: h.Text,
				At:   time.Unix(h.At, 0),
			})
			n++
		}
		if len(dst) == 0 {
			continue
		}
		b.hist[irccase.Fold(ch)] = dst
	}
	return n, nil
}
