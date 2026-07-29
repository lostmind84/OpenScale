package update

import (
	"context"
	"errors"
	"time"
)

// ErrNoRelease reports a repository that has published no stable release.
//
// It is NOT a breakdown, and the distinction matters on the screen: a fork that
// has only published prereleases answers 404 on /releases/latest, and a station
// that follows it simply has nothing to offer. A rate limit or a proxy, by
// contrast, is ErrUnreachable -- reading either as « no release » would tell a
// station it is up to date when nobody knows.
var ErrNoRelease = errors.New("update: repository has published no release")

// ErrUnreachable reports that the release server could not be read: no route, a
// proxy answering HTML, a spent rate limit, a timeout, a body that is not JSON.
var ErrUnreachable = errors.New("update: release server unreachable")

// Asset is one file attached to a release.
type Asset struct {
	Name string
	// URL is browser_download_url and never the API handle: the latter answers
	// JSON describing the asset rather than the asset itself.
	URL  string
	Size int64
}

// Release is a published version a station could move to.
type Release struct {
	// Tag is the git tag AS PUBLISHED -- « v0.1 » and « 2.1.0 » both occur -- and
	// the archive names are built from this, not from Version.String().
	Tag         string
	Version     Version
	PublishedAt time.Time
	HTMLURL     string
	Assets      []Asset
}

// Asset returns the attached file of that exact name.
func (r Release) Asset(name string) (Asset, bool) {
	for _, candidate := range r.Assets {
		if candidate.Name == name {
			return candidate, true
		}
	}
	return Asset{}, false
}

// Source reports the newest release a station could move to.
//
// It is declared here, on the consumer's side, so that a test answers without a
// network and so that a repository served by something other than GitHub could be
// added one day without touching what consumes it.
type Source interface {
	// Latest returns the newest stable release, or ErrNoRelease when the
	// repository has published none.
	Latest(ctx context.Context) (Release, error)
}
