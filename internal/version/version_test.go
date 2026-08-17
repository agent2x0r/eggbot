package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var semver = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

func TestUserAgent(t *testing.T) {
	if UserAgent() != "eggbot-"+Version {
		t.Fatal(UserAgent())
	}
	if !strings.Contains(String(), Version) {
		t.Fatal(String())
	}
}

func TestVersionShape(t *testing.T) {
	if !semver.MatchString(Version) {
		t.Fatalf("Version %q is not X.Y.Z", Version)
	}
}

func TestChangelogDocumentsThisVersion(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	heading := "## " + Version
	if !strings.Contains(string(raw), heading) {
		t.Fatalf("CHANGELOG.md must contain %q", heading)
	}
}
