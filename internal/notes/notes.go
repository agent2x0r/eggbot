// Package notes is the eggdrop-style offline message list (handle to handle).
package notes

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"eggbot/internal/irccase"
	"eggbot/internal/store"
)

const maxPerUser = 20

type Note struct {
	ID        int64
	To        string
	From      string
	Body      string
	CreatedAt time.Time
}

type Module struct {
	st *store.Store
}

func New(st *store.Store) *Module { return &Module{st: st} }

func (m *Module) Add(to, from, body string) error {
	to = irccase.Fold(to)
	from = irccase.Fold(from)
	body = stringsTrim(body)
	if to == "" || body == "" {
		return fmt.Errorf("usage: note <handle> <text>")
	}
	return m.st.Tx(context.Background(), func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM notes WHERE to_handle = ? AND read = 0`, to).Scan(&n); err != nil {
			return err
		}
		if n >= maxPerUser {
			return fmt.Errorf("%s's inbox is full", to)
		}
		_, err := tx.Exec(
			`INSERT INTO notes (to_handle, from_handle, body, created_at, read) VALUES (?, ?, ?, ?, 0)`,
			to, from, body, store.Now(),
		)
		return err
	})
}

func (m *Module) Pending(handle string) []Note {
	rows, err := m.st.DB.Query(
		`SELECT id, to_handle, from_handle, body, created_at FROM notes WHERE to_handle = ? AND read = 0 ORDER BY id`,
		irccase.Fold(handle),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		var n Note
		var ts int64
		if err := rows.Scan(&n.ID, &n.To, &n.From, &n.Body, &ts); err != nil {
			continue
		}
		n.CreatedAt = time.Unix(ts, 0)
		out = append(out, n)
	}
	return out
}

func (m *Module) MarkRead(handle string) {
	_, _ = m.st.DB.Exec(`UPDATE notes SET read = 1 WHERE to_handle = ?`, irccase.Fold(handle))
}

func stringsTrim(s string) string {
	if len(s) > 400 {
		s = s[:400]
	}
	return s
}
