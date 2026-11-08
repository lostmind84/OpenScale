package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultBaseURL is where releases are read.
//
// IT IS COMPILED IN and never comes from the configuration. What a file may name
// is the repository, an owner/repo pair checked by control 48; letting it name a
// host would turn « save the configuration » into « download code from anywhere,
// and run it as LocalSystem ».
const DefaultBaseURL = "https://api.github.com"

// latestTimeout bounds one poll. The station polls once a day, and a shop whose
// line is down must not leave a request hanging behind it.
const latestTimeout = 30 * time.Second

// maxPayload caps what one answer may be. An API answer is a few kilobytes; a
// captive portal can serve megabytes, and this station has better uses for them.
const maxPayload = 1 << 20

// GitHubSource reads the releases of one repository.
type GitHubSource struct {
	// Repository is an owner/repo pair, checked by control 48 long before it
	// gets here.
	Repository string
	// BaseURL overrides the API root. Empty means DefaultBaseURL; a test points
	// it at an httptest.Server.
	BaseURL string
	// Client overrides the HTTP client. Nil means one bounded by latestTimeout.
	Client *http.Client
}

// latestPayload is the part of the API answer this station reads, and no more.
//
// Nothing here decodes `draft` or `prerelease`: /releases/latest excludes both by
// contract, which is exactly why that endpoint is used instead of /releases.
type latestPayload struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		// URL is the API handle of the asset, not browser_download_url: on
		// 08/11/2026 github.com answered 504 to every anonymous download of a
		// release file while api.github.com served the same bytes. It answers the
		// bytes only to a request that asks for them — see AssetClient.
		URL string `json:"url"`
	} `json:"assets"`
}

// Latest returns the newest stable release of the repository.
func (g GitHubSource) Latest(ctx context.Context) (Release, error) {
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: latestTimeout}
	}
	base := g.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	url := fmt.Sprintf("%s/repos/%s/releases/latest", base, g.Repository)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := client.Do(request)
	if err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer func() { _ = response.Body.Close() }()

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// The repository exists and has published nothing stable. Not a fault.
		return Release{}, ErrNoRelease
	default:
		return Release{}, fmt.Errorf("%w: statut %d", ErrUnreachable, response.StatusCode)
	}

	var payload latestPayload
	if err := json.NewDecoder(io.LimitReader(response.Body, maxPayload)).
		Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	version, err := ParseVersion(payload.TagName)
	if err != nil {
		return Release{}, err
	}
	release := Release{
		Tag: payload.TagName, Version: version,
		PublishedAt: payload.PublishedAt, HTMLURL: payload.HTMLURL,
	}
	for _, asset := range payload.Assets {
		release.Assets = append(release.Assets,
			Asset{Name: asset.Name, URL: asset.URL, Size: asset.Size})
	}
	return release, nil
}

// AssetClient is the HTTP client a Stager downloads release files with.
//
// It asks every request for application/octet-stream, because the API handle of
// an asset (latestPayload's URL) answers the asset's JSON DESCRIPTION otherwise,
// and the digest check would then reject a JSON document instead of an archive.
func AssetClient() *http.Client {
	return &http.Client{Timeout: downloadTimeout, Transport: octetStream{http.DefaultTransport}}
}

// octetStream adds the Accept header the asset API needs, to the first request and
// to the redirect GitHub answers it with.
type octetStream struct{ next http.RoundTripper }

func (o octetStream) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Accept", "application/octet-stream")
	return o.next.RoundTrip(request)
}
