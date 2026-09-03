// Package userfile is users, hostmasks, flags, and argon2id passwords.
package userfile

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"eggbot/internal/flags"
	"eggbot/internal/hostmask"
	"eggbot/internal/irccase"
	"eggbot/internal/pass"
	"eggbot/internal/store"
)

var (
	ErrExists         = fmt.Errorf("user already exists")
	ErrNotFound       = fmt.Errorf("no such user")
	ErrPermanentOwner = fmt.Errorf("cannot modify permanent owner that way")
	ErrLastOwner      = fmt.Errorf("cannot remove the last owner")
	ErrBadFlags       = fmt.Errorf("invalid flags")
)

const (
	MinPasswordRunes = 8
	MaxPasswordBytes = 1024
)

func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordRunes {
		return fmt.Errorf("password must be at least %d characters", MinPasswordRunes)
	}
	if len(password) > MaxPasswordBytes {
		return fmt.Errorf("password is too long")
	}
	return nil
}

type User struct {
	Handle    string
	Password  string
	Global    flags.Set
	CreatedAt time.Time
	LastSeen  time.Time
	Hosts     []string
	ChanFlags map[string]flags.Set // folded channel -> flags
}

func (u *User) HasPass() bool { return u.Password != "" }

type File struct {
	mu     sync.RWMutex
	st     *store.Store
	owners []string // folded permanent owners from config
}

func New(st *store.Store, permanentOwners []string) *File {
	owners := make([]string, 0, len(permanentOwners))
	for _, o := range permanentOwners {
		owners = append(owners, irccase.Fold(o))
	}
	return &File{st: st, owners: owners}
}

func (f *File) SeedOwners() error {
	for _, h := range f.owners {
		if h == "" {
			continue
		}
		u, err := f.Get(h)
		if err == ErrNotFound {
			if err := f.Add(h, ""); err != nil {
				return err
			}
			u, err = f.Get(h)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if !u.Global.Has(flags.Owner) || !u.Global.Has(flags.Master) || !u.Global.Has(flags.Party) {
			u.Global.Add(flags.Owner)
			u.Global.Add(flags.Master)
			u.Global.Add(flags.Party)
			if err := f.saveUser(u); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *File) IsPermanentOwner(handle string) bool {
	h := irccase.Fold(handle)
	for _, o := range f.owners {
		if o == h {
			return true
		}
	}
	return false
}

func (f *File) Add(handle, password string) error {
	handle = irccase.Fold(strings.TrimSpace(handle))
	if handle == "" {
		return fmt.Errorf("empty handle")
	}
	hash := ""
	if password != "" {
		if err := ValidatePassword(password); err != nil {
			return err
		}
		var err error
		hash, err = pass.Hash(password)
		if err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := f.st.DB.Exec(
		`INSERT INTO users (handle, password, global_flags, created_at, last_seen) VALUES (?, ?, '', ?, 0)`,
		handle, hash, store.Now(),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrExists
		}
		return err
	}
	return nil
}

func (f *File) Delete(handle string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	handle = irccase.Fold(handle)
	if f.IsPermanentOwner(handle) {
		return ErrPermanentOwner
	}
	u, err := f.Get(handle)
	if err != nil {
		return err
	}
	if u.Global.Has(flags.Owner) {
		n, err := f.countOwners()
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastOwner
		}
	}
	_, err = f.st.DB.Exec(`DELETE FROM users WHERE handle = ?`, handle)
	return err
}

func (f *File) Get(handle string) (*User, error) {
	handle = irccase.Fold(handle)
	u := &User{Handle: handle, Global: flags.Set{}, ChanFlags: map[string]flags.Set{}}
	var gflags string
	var created, last int64
	err := f.st.DB.QueryRow(
		`SELECT password, global_flags, created_at, last_seen FROM users WHERE handle = ?`,
		handle,
	).Scan(&u.Password, &gflags, &created, &last)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Global = flags.Parse(gflags)
	u.CreatedAt = time.Unix(created, 0)
	if last > 0 {
		u.LastSeen = time.Unix(last, 0)
	}
	rows, err := f.st.DB.Query(`SELECT mask FROM hosts WHERE handle = ?`, handle)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			rows.Close()
			return nil, err
		}
		u.Hosts = append(u.Hosts, m)
	}
	rows.Close()
	crows, err := f.st.DB.Query(`SELECT channel, flags FROM channel_flags WHERE handle = ?`, handle)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var ch, fl string
		if err := crows.Scan(&ch, &fl); err != nil {
			return nil, err
		}
		u.ChanFlags[irccase.Fold(ch)] = flags.Parse(fl)
	}
	return u, nil
}

func (f *File) List() ([]*User, error) {
	rows, err := f.st.DB.Query(`SELECT handle FROM users ORDER BY handle`)
	if err != nil {
		return nil, err
	}
	var handles []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return nil, err
		}
		handles = append(handles, h)
	}
	rows.Close()
	var out []*User
	for _, h := range handles {
		u, err := f.Get(h)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func (f *File) FindByHost(nuh string) *User {
	rows, err := f.st.DB.Query(`SELECT handle, mask FROM hosts`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	bestHandle := ""
	bestScore := -1
	for rows.Next() {
		var handle, mask string
		if err := rows.Scan(&handle, &mask); err != nil {
			continue
		}
		if hostmask.Match(mask, nuh) {
			sc := hostmask.Specificity(mask)
			if sc > bestScore {
				bestScore = sc
				bestHandle = handle
			}
		}
	}
	if bestHandle == "" {
		return nil
	}
	u, err := f.Get(bestHandle)
	if err != nil {
		return nil
	}
	return u
}

func (f *File) CheckPass(handle, password string) bool {
	u, err := f.Get(handle)
	if err != nil || u.Password == "" {
		pass.DummyVerify(password)
		return false
	}
	return pass.Verify(password, u.Password)
}

func (f *File) SetPass(handle, password string) error {
	handle = irccase.Fold(handle)
	hash := ""
	if password != "" {
		if err := ValidatePassword(password); err != nil {
			return err
		}
		var err error
		hash, err = pass.Hash(password)
		if err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.Get(handle); err != nil {
		return err
	}
	_, err := f.st.DB.Exec(`UPDATE users SET password = ? WHERE handle = ?`, hash, handle)
	return err
}

func (f *File) AddHost(handle, mask string) error {
	handle = irccase.Fold(handle)
	mask = strings.TrimSpace(mask)
	if mask == "" {
		return fmt.Errorf("empty hostmask")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.Get(handle); err != nil {
		return err
	}
	_, err := f.st.DB.Exec(`INSERT OR IGNORE INTO hosts (handle, mask) VALUES (?, ?)`, handle, mask)
	return err
}

func (f *File) DelHost(handle, mask string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	handle = irccase.Fold(handle)
	res, err := f.st.DB.Exec(`DELETE FROM hosts WHERE handle = ? AND mask = ?`, handle, mask)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no such hostmask")
	}
	return nil
}

func (f *File) Chattr(handle, spec, channel string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.Get(handle)
	if err != nil {
		return nil, err
	}
	if channel == "" {
		next := u.Global.Clone()
		if err := next.Apply(spec); err != nil {
			return nil, err
		}
		if u.Global.Has(flags.Owner) && !next.Has(flags.Owner) {
			if f.IsPermanentOwner(u.Handle) {
				return nil, ErrPermanentOwner
			}
			n, err := f.countOwners()
			if err != nil {
				return nil, err
			}
			if n <= 1 {
				return nil, ErrLastOwner
			}
		}
		u.Global = next
		if err := f.saveUser(u); err != nil {
			return nil, err
		}
		return u, nil
	}
	ch := irccase.Fold(channel)
	cur := u.ChanFlags[ch]
	if cur == nil {
		cur = flags.Set{}
	}
	if err := cur.Apply(spec); err != nil {
		return nil, err
	}
	if cur.Empty() {
		_, err = f.st.DB.Exec(`DELETE FROM channel_flags WHERE handle = ? AND channel = ?`, u.Handle, ch)
	} else {
		_, err = f.st.DB.Exec(
			`INSERT INTO channel_flags (handle, channel, flags) VALUES (?, ?, ?)
			 ON CONFLICT(handle, channel) DO UPDATE SET flags = excluded.flags`,
			u.Handle, ch, cur.String(),
		)
	}
	if err != nil {
		return nil, err
	}
	u.ChanFlags[ch] = cur
	return u, nil
}

func (f *File) Touch(handle string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	handle = irccase.Fold(handle)
	_, err := f.st.DB.Exec(`UPDATE users SET last_seen = ? WHERE handle = ?`, store.Now(), handle)
	if err != nil {
		f.st.NoteWriteError()
		return err
	}
	return nil
}

func (f *File) MatchAttr(u *User, expr, channel string) bool {
	if u == nil {
		return flags.MatchAttr(nil, nil, expr)
	}
	var ch flags.Set
	if channel != "" {
		ch = u.ChanFlags[irccase.Fold(channel)]
	}
	return flags.MatchAttr(u.Global, ch, expr)
}

func (f *File) saveUser(u *User) error {
	_, err := f.st.DB.Exec(
		`UPDATE users SET global_flags = ?, last_seen = ? WHERE handle = ?`,
		u.Global.String(), store.Now(), u.Handle,
	)
	return err
}

func (f *File) countOwners() (int, error) {
	rows, err := f.st.DB.Query(`SELECT global_flags FROM users`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return 0, err
		}
		if flags.Parse(g).Has(flags.Owner) {
			n++
		}
	}
	return n, nil
}

func (f *File) ChannelFlags(u *User, channel string) flags.Set {
	if u == nil {
		return nil
	}
	return u.ChanFlags[irccase.Fold(channel)]
}
