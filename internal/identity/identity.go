package identity

import (
	"database/sql"
	"fmt"
	"strings"

	"eggbot/internal/irccase"
	"eggbot/internal/store"
)

const (
	MechAccount = "account"
	MechCertFP  = "certfp"
	MechHost    = "host"
)

type Binding struct {
	Handle     string
	Network    string
	Mechanism  string
	ExternalID string
}

type Registry struct {
	st *store.Store
}

func New(st *store.Store) *Registry { return &Registry{st: st} }

func (r *Registry) Bind(handle, network, mech, ext string) error {
	handle = irccase.HandleFold(handle)
	network = strings.TrimSpace(network)
	mech = strings.ToLower(strings.TrimSpace(mech))
	ext = strings.TrimSpace(ext)
	if mech == MechCertFP {
		ext = strings.ToLower(ext)
	}
	if handle == "" || network == "" || mech == "" || ext == "" {
		return fmt.Errorf("incomplete identity")
	}
	_, err := r.st.DB.Exec(
		`INSERT INTO identities (handle, network, mechanism, external_id, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(network, mechanism, external_id) DO UPDATE SET handle=excluded.handle`,
		handle, network, mech, ext, store.Now(),
	)
	return err
}

func (r *Registry) Lookup(network, mech, ext string) (string, bool) {
	var handle string
	err := r.st.DB.QueryRow(
		`SELECT handle FROM identities WHERE network = ? AND mechanism = ? AND external_id = ?`,
		network, strings.ToLower(mech), strings.TrimSpace(ext),
	).Scan(&handle)
	if err == sql.ErrNoRows || err != nil {
		return "", false
	}
	return handle, true
}

func (r *Registry) Unbind(handle, network, mech, ext string) error {
	_, err := r.st.DB.Exec(
		`DELETE FROM identities WHERE handle = ? AND network = ? AND mechanism = ? AND external_id = ?`,
		irccase.HandleFold(handle), network, strings.ToLower(mech), ext,
	)
	return err
}

func (r *Registry) ForHandle(handle string) []Binding {
	rows, err := r.st.DB.Query(
		`SELECT handle, network, mechanism, external_id FROM identities WHERE handle = ?`,
		irccase.HandleFold(handle),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Binding
	for rows.Next() {
		var b Binding
		if err := rows.Scan(&b.Handle, &b.Network, &b.Mechanism, &b.ExternalID); err != nil {
			continue
		}
		out = append(out, b)
	}
	return out
}

// Resolve prefers account, then certfp. Hostmask fallback is handled by the caller.
func (r *Registry) Resolve(network, account, certfp string) (handle, mech string) {
	if account != "" && account != "*" {
		if h, ok := r.Lookup(network, MechAccount, account); ok {
			return h, MechAccount
		}
	}
	if certfp != "" {
		if h, ok := r.Lookup(network, MechCertFP, strings.ToLower(certfp)); ok {
			return h, MechCertFP
		}
	}
	return "", ""
}
