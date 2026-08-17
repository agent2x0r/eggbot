# Releasing

The version string is `Version` in `internal/version/version.go`. `make` embeds it. `eggbot -version`, partyline `.version`, and CTCP VERSION (`eggbot-0.1.0`) all use it. `CHANGELOG.md` must have a `##` heading with the same number; a unit test checks this.

To release: update `Version`, add a changelog section, run `make check`, run `make`, tag `vX.Y.Z`.

Version numbers follow the usual pattern: patch for fixes (`0.1.0` → `0.1.1`), minor for features (`0.2.0`), major if configuration or protocol compatibility breaks. A suffix such as `0.2.0-pre` is allowed.

`make VERSION=...` rebuilds with an explicit stamp. For a real release, edit `version.go` instead.

Optional: `make sbom` and `shasum -a 256 eggbot > SHA256SUMS`.
