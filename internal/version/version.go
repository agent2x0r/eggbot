// Package version is the single source of truth for eggbot's release number.
//
// Bump Version here, add a matching ## section in CHANGELOG.md, then tag vX.Y.Z.
// Makefile reads Version for ldflags. Commit and Date are stamped at build time.
// The running bot reports this string via:
//
//	eggbot -version
//	partyline .version (also .status)
//	CTCP VERSION (eggbot-<Version>)
package version

import "fmt"

// These are overridden by -ldflags at build time. Version must stay in sync
// with CHANGELOG.md; the unit test enforces that.
var (
	Version = "0.1.1"
	Commit  = "unknown"
	Date    = "unknown"
)

func UserAgent() string {
	return "eggbot-" + Version
}

func String() string {
	return fmt.Sprintf("eggbot %s commit=%s date=%s", Version, Commit, Date)
}
