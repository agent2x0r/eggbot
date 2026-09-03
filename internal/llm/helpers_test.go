package llm

import (
	"errors"
	"strings"
	"testing"

	"eggbot/internal/flags"
	"eggbot/internal/userfile"
)

func TestHelpers(t *testing.T) {
	if !SilentReply("SILENT") || !SilentReply("  silent  ") || SilentReply("hello") {
		t.Fatal("silent")
	}
	if !DropContext("DROP") || SilentReply("DROP") || DropContext("hello") {
		t.Fatal("drop")
	}
	if friendlyErr(nil) != nil {
		t.Fatal("nil")
	}
	if friendlyErr(errors.New("deadline exceeded")).Error() == "" {
		t.Fatal("timeout")
	}
	if friendlyErr(errors.New("429 rate-limited")).Error() == "" {
		t.Fatal("429")
	}
	if friendlyErr(&ProviderError{Status: 500, Body: "boom"}).Error() != "couldn't look that up right now" {
		t.Fatal("provider")
	}
	if friendlyErr(&ProviderError{Status: 429}).Error() != "AI is busy, try in a minute" {
		t.Fatal("429 typed")
	}
	if friendlyErr(errors.New("nope")).Error() == "" {
		t.Fatal("generic")
	}
	_ = clip("hi", 0)
	_ = clip(strings.Repeat("x", 20), 5)
	_ = clip("🙂🙂🙂", 4)
	_ = clip("see https://github.com/foo/bar extra", 20)
	u := &userfile.User{Handle: "n", Global: flags.Parse("n")}
	_ = FlagCheck(nil, "-", "")
	_ = FlagCheck(u, "n", "")
	_ = FlagCheck(u, "o", "#c")
	_ = functionTool(FuncTool("seen_lookup", "seen", Schema(map[string]any{"nick": Prop("string", "n")}, []string{"nick"})))
	_ = webSearchTool()
	_ = xSearchTool()
}
