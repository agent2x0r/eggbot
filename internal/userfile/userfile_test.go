package userfile

import (
	"path/filepath"
	"testing"

	"eggbot/internal/flags"
	"eggbot/internal/store"
)

func TestUserfile(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	f := New(st, []string{"nate"})
	if err := f.SeedOwners(); err != nil {
		t.Fatal(err)
	}
	u, err := f.Get("nate")
	if err != nil || !u.Global.Has(flags.Owner) {
		t.Fatalf("owner seed: %+v %v", u, err)
	}
	if err := f.Delete("nate"); err != ErrPermanentOwner {
		t.Fatalf("expected permanent owner, got %v", err)
	}
	if err := f.Add("bob", "hunter22"); err != nil {
		t.Fatal(err)
	}
	if !f.CheckPass("bob", "hunter22") {
		t.Fatal("pass")
	}
	if err := f.SetPass("bob", "short"); err == nil {
		t.Fatal("expected weak password to be rejected")
	}
	if err := f.AddHost("bob", "bob!*@*.example"); err != nil {
		t.Fatal(err)
	}
	if got := f.FindByHost("bob!x@foo.example"); got == nil || got.Handle != "bob" {
		t.Fatalf("find: %+v", got)
	}
	if _, err := f.Chattr("bob", "+of", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Chattr("nate", "-n", ""); err != ErrPermanentOwner && err != ErrLastOwner {
		t.Fatalf("strip owner: %v", err)
	}
}
