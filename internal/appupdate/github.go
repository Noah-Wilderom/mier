package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"
	"golang.org/x/mod/semver"
)

const releaseRepository = "Noah-Wilderom/mier"
const releasesURL = "https://api.github.com/repos/" + releaseRepository + "/releases"
const releaseDownloadURL = "https://github.com/" + releaseRepository + "/releases/download/"
const maxReleasePageBytes = 8 * 1024 * 1024

// Discovery uses release tags because GitHub's /latest endpoint excludes
// prereleases. Wails still parses the signed manifest, downloads, verifies,
// stages, and replaces the application through its existing endpoint adapter.
type githubProvider struct {
	channel    string
	client     *http.Client
	downloader *endpoint.Provider
}

func newGitHubProvider(channel string, client *http.Client) (*githubProvider, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	downloader, err := endpoint.New(endpoint.Config{URL: releaseDownloadURL, HTTPClient: client})
	if err != nil {
		return nil, err
	}
	return &githubProvider{channel: channel, client: client, downloader: downloader}, nil
}

func (p *githubProvider) Name() string { return "mier-github" }

type githubRelease struct {
	Tag        string        `json:"tag_name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Published  *time.Time    `json:"published_at"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type releaseCandidate struct {
	version, manifest string
}

func (p *githubProvider) Check(ctx context.Context, request updater.CheckRequest) (*updater.Release, error) {
	channel, err := releaseChannel(request.CurrentVersion)
	if err != nil || channel != p.channel {
		return nil, errors.New("update request does not match the installed release channel")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	candidate, err := p.latest(ctx, request.CurrentVersion)
	if err != nil || candidate == nil {
		return nil, err
	}
	// Old M.I.E.R. prereleases were incorrectly labelled "stable" in their
	// manifests. Enforce the tag/version channel ourselves so those signed
	// manifests stay usable without admitting another prerelease channel.
	provider, err := endpoint.New(endpoint.Config{URL: candidate.manifest, HTTPClient: p.client})
	if err != nil {
		return nil, err
	}
	release, err := provider.Check(ctx, request)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, errors.New("published update manifest is unavailable or does not advertise its release tag")
	}
	actualChannel, err := releaseChannel(release.Version)
	if err != nil || actualChannel != p.channel || release.Version != candidate.version {
		return nil, errors.New("update manifest version does not match its GitHub release tag and channel")
	}
	if release.Channel != "" && release.Channel != p.channel && !(release.Channel == "stable" && p.channel != "stable") {
		return nil, errors.New("update manifest declares a different release channel")
	}
	release.Channel = p.channel
	return release, nil
}

func (p *githubProvider) latest(ctx context.Context, current string) (*releaseCandidate, error) {
	var best *releaseCandidate
	for page := 1; page <= 100; page++ {
		releases, err := p.page(ctx, page)
		if err != nil {
			return nil, err
		}
		for _, release := range releases {
			channel, err := releaseChannel(release.Tag)
			if err != nil || release.Draft || release.Published == nil || channel != p.channel || release.Prerelease != (channel != "stable") {
				continue
			}
			version := strings.TrimPrefix(release.Tag, "v")
			if semver.Compare("v"+version, "v"+strings.TrimPrefix(current, "v")) <= 0 || (best != nil && semver.Compare("v"+version, "v"+best.version) <= 0) {
				continue
			}
			for _, asset := range release.Assets {
				if asset.Name == "manifest.json" && asset.Size > 0 && validManifestURL(asset.URL, release.Tag) {
					best = &releaseCandidate{version: version, manifest: asset.URL}
					break
				}
			}
		}
		if len(releases) < 100 {
			return best, nil
		}
	}
	return nil, errors.New("GitHub update release pagination exceeded 100 pages")
}

func validManifestURL(raw, tag string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "github.com" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Path == "/"+releaseRepository+"/releases/download/"+tag+"/manifest.json"
}

func (p *githubProvider) page(ctx context.Context, page int) ([]githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=100&page=%d", releasesURL, page), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "MIER-updater")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("read GitHub update releases: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub update release lookup returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxReleasePageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReleasePageBytes {
		return nil, errors.New("GitHub update release response exceeded the size limit")
	}
	var releases []githubRelease
	if err := json.Unmarshal(data, &releases); err != nil || releases == nil {
		return nil, errors.New("GitHub returned an invalid update release list")
	}
	return releases, nil
}

func (p *githubProvider) Download(ctx context.Context, release *updater.Release, destination io.Writer, progress func(int64, int64)) error {
	return p.downloader.Download(ctx, release, destination, progress)
}
