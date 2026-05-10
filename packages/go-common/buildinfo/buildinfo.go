// Package buildinfo exposes the version-control metadata embedded in
// the running binary by `go build`. Used in service boot logs so an
// operator can confirm the deployed binary matches the expected commit
// without having to ssh into the machine and inspect the image label.
//
// Go embeds vcs.revision, vcs.time, and vcs.modified automatically when
// you run `go build` from inside a git checkout. There is nothing to
// pass via -ldflags; this is the canonical replacement for the
// "AUTH_BUILD_MARKER=…" hack we used while debugging the Fly deploy.
package buildinfo

import (
	"runtime/debug"
	"time"
)

// Revision returns the git commit SHA the binary was built from, or
// "unknown" if the build context didn't include VCS info (rare; means
// the binary was built outside a checkout, e.g. from a tarball).
func Revision() string {
	if r, ok := vcs("vcs.revision"); ok {
		return r
	}
	return "unknown"
}

// Time returns the commit timestamp from VCS, or the zero time if
// unavailable. Useful for "is this binary from before or after the
// outage?" forensics.
func Time() time.Time {
	if s, ok := vcs("vcs.time"); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Modified returns true if the working tree had uncommitted changes
// when the binary was built. A `true` here in production is a red
// flag: it means someone built and shipped from a dirty checkout, so
// the SHA in Revision() does not fully describe what's running.
func Modified() bool {
	if s, ok := vcs("vcs.modified"); ok {
		return s == "true"
	}
	return false
}

// Short returns the first 8 characters of Revision() — convenient for
// log lines where the full 40-char SHA is overkill.
func Short() string {
	r := Revision()
	if len(r) >= 8 {
		return r[:8]
	}
	return r
}

func vcs(key string) (string, bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, s := range bi.Settings {
		if s.Key == key {
			return s.Value, true
		}
	}
	return "", false
}
