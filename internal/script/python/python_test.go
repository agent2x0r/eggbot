package python

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"eggbot/internal/script"
)

func TestPythonRequiresExplicitTrust(t *testing.T) {
	h := New(script.New(nil, slog.Default(), nil), "python3", "")
	if err := h.LoadFile(filepath.Join(t.TempDir(), "plugin.py")); err == nil {
		t.Fatal("expected untrusted Python plugin to be rejected before execution")
	}
}

func TestPythonReloadStopsPreviousProcess(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-python")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho '{\"op\":\"hello\"}'\nwhile IFS= read -r line; do :; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(dir, "plugin.py")
	if err := os.WriteFile(plugin, []byte("# test fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	eng := script.New(nil, slog.Default(), []string{plugin})
	h := New(eng, bin, "")
	defer h.Close()

	if err := h.LoadFile(plugin); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	old := h.procs[plugin]
	h.mu.Unlock()
	if old == nil {
		t.Fatal("missing first process")
	}
	if err := h.LoadFile(plugin); err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.done:
	case <-time.After(2 * time.Second):
		t.Fatal("previous Python process was not stopped on reload")
	}
	h.mu.Lock()
	current := h.procs[plugin]
	h.mu.Unlock()
	if current == nil || current == old {
		t.Fatal("reload did not install a new process")
	}
}
