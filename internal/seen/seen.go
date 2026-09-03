// Package seen records the last time a nick spoke, joined, or quit.
package seen

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"eggbot/internal/irccase"
	"eggbot/internal/store"
)

type Record struct {
	Nick     string
	Handle   string
	Account  string
	Host     string
	Channel  string
	Event    string
	LastText string
	SeenAt   time.Time
}

type Module struct {
	st *store.Store
}

func New(st *store.Store) *Module { return &Module{st: st} }

func (m *Module) Record(nick, handle, account, host, channel, event, text string) error {
	nickF := irccase.Fold(nick)
	if len(text) > 200 {
		text = text[:200]
	}
	_, err := m.st.DB.Exec(
		`INSERT INTO seen (nick, handle, account, host, channel, event, last_text, seen_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(nick) DO UPDATE SET handle=excluded.handle, account=excluded.account, host=excluded.host,
		   channel=excluded.channel, event=excluded.event, last_text=excluded.last_text, seen_at=excluded.seen_at`,
		nickF, irccase.Fold(handle), account, host, channel, event, text, store.Now(),
	)
	if err != nil {
		m.st.NoteWriteError()
		return err
	}
	return nil
}

func (m *Module) Lookup(nick string) (*Record, error) {
	var r Record
	var ts int64
	err := m.st.DB.QueryRow(
		`SELECT nick, handle, account, host, channel, event, last_text, seen_at FROM seen WHERE nick = ?`,
		irccase.Fold(nick),
	).Scan(&r.Nick, &r.Handle, &r.Account, &r.Host, &r.Channel, &r.Event, &r.LastText, &ts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.SeenAt = time.Unix(ts, 0)
	return &r, nil
}

func (m *Module) Format(want string, r *Record) string {
	if r == nil {
		return fmt.Sprintf("i haven't seen %s", want)
	}
	ago := time.Since(r.SeenAt).Truncate(time.Second)
	msg := fmt.Sprintf("%s was last seen %s ago", want, human(ago))
	if r.Channel != "" {
		msg += " in " + r.Channel
	}
	if r.Event != "" {
		msg += " (" + r.Event + ")"
	}
	if r.LastText != "" {
		msg += `: "` + r.LastText + `"`
	}
	return msg
}

func human(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	return fmt.Sprintf("%dd%dh", days, int(d.Hours())%24)
}

func Search(s string) string { return strings.TrimSpace(s) }
