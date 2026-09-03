package userfile

import (
	"path/filepath"
	"testing"

	"eggbot/internal/store"
)

func TestListHostsTouchMatch(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	f := New(st, []string{"nate"})
	if err := f.SeedOwners(); err != nil {
		t.Fatal(err)
	}
	if err := f.Add("bob", "hunter22"); err != nil {
		t.Fatal(err)
	}
	if err := f.AddHost("bob", "bob!*@*.example"); err != nil {
		t.Fatal(err)
	}
	u, err := f.Get("bob")
	if err != nil {
		t.Fatal(err)
	}
	if !u.HasPass() {
		t.Fatal("pass")
	}
	if err := f.Touch("bob"); err != nil {
		t.Fatal(err)
	}
	_ = f.MatchAttr(u, "-", "")
	_ = f.MatchAttr(u, "p", "")
	_ = f.MatchAttr(nil, "-", "")
	_ = f.ChannelFlags(u, "#c")
	_ = f.ChannelFlags(nil, "#c")
	list, err := f.List()
	if err != nil || len(list) < 2 {
		t.Fatalf("list %v %v", list, err)
	}
	if err := f.DelHost("bob", "bob!*@*.example"); err != nil {
		t.Fatal(err)
	}
	if err := f.DelHost("bob", "missing!*@*"); err == nil {
		t.Fatal("missing host")
	}
	if _, err := f.Chattr("bob", "+f", "#chan"); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete("bob"); err != nil {
		t.Fatal(err)
	}
}

func TestTouchErrorOnClosedStore(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	f := New(st, []string{"nate"})
	if err := f.SeedOwners(); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if err := f.Touch("nate"); err == nil {
		t.Fatal("expected last_seen error on closed store")
	}
	if st.WriteErrors() == 0 {
		t.Fatal("closed touch should count a write error")
	}
}
