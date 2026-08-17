// Package quotes stores per-channel one-liners.
package quotes

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"eggbot/internal/irccase"
	"eggbot/internal/store"
)

type Quote struct {
	ID        int64
	Channel   string
	Nick      string
	Body      string
	CreatedAt time.Time
}

type Module struct {
	st *store.Store
}

func New(st *store.Store) *Module { return &Module{st: st} }

func (m *Module) Add(channel, nick, body string) (int64, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return 0, fmt.Errorf("empty quote")
	}
	res, err := m.st.DB.Exec(
		`INSERT INTO quotes (channel, nick, body, created_at) VALUES (?, ?, ?, ?)`,
		irccase.Fold(channel), nick, body, store.Now(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *Module) Delete(id int64) error {
	res, err := m.st.DB.Exec(`DELETE FROM quotes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *Module) Random(channel, search string) (*Quote, error) {
	var q Quote
	var ts int64
	var err error
	if search == "" {
		err = m.st.DB.QueryRow(
			`SELECT id, channel, nick, body, created_at FROM quotes WHERE channel = ? ORDER BY RANDOM() LIMIT 1`,
			irccase.Fold(channel),
		).Scan(&q.ID, &q.Channel, &q.Nick, &q.Body, &ts)
	} else {
		err = m.st.DB.QueryRow(
			`SELECT id, channel, nick, body, created_at FROM quotes WHERE channel = ? AND body LIKE ? ORDER BY RANDOM() LIMIT 1`,
			irccase.Fold(channel), "%"+search+"%",
		).Scan(&q.ID, &q.Channel, &q.Nick, &q.Body, &ts)
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	q.CreatedAt = time.Unix(ts, 0)
	return &q, nil
}

func (q *Quote) Format() string {
	return fmt.Sprintf("quote #%d <%s> %s", q.ID, q.Nick, q.Body)
}
