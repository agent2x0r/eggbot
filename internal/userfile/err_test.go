package userfile

import (
	"path/filepath"
	"strings"
	"testing"

	"eggbot/internal/store"
)

func TestAddDeletePassErrors(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	f := New(st, []string{"nate", ""})
	if err := f.SeedOwners(); err != nil {
		t.Fatal(err)
	}
	if err := f.SeedOwners(); err != nil {
		t.Fatal(err)
	}
	if err := f.Add("", ""); err == nil {
		t.Fatal("empty")
	}
	if err := f.Add("nate", ""); err != ErrExists {
		t.Fatal(err)
	}
	if err := f.Add("bob", "short"); err == nil {
		t.Fatal("short pass")
	}
	if err := f.Add("bob", "hunter22"); err != nil {
		t.Fatal(err)
	}
	if f.CheckPass("missing", "x") || f.CheckPass("nate", "x") || !f.CheckPass("bob", "hunter22") {
		t.Fatal("check")
	}
	if err := f.Delete("nate"); err != ErrPermanentOwner {
		t.Fatal(err)
	}
	if err := f.Delete("missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := f.Chattr("bob", "+n", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete("bob"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePassword(strings.Repeat("x", MaxPasswordBytes+1)); err == nil {
		t.Fatal("too long")
	}
	if _, err := f.Get("missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := f.SetPass("missing", "hunter22"); err == nil {
		t.Fatal("set missing")
	}
	if err := f.AddHost("missing", "*!*@*"); err == nil {
		t.Fatal("host missing")
	}
	if err := f.AddHost("nate", ""); err == nil {
		t.Fatal("empty mask")
	}
}
