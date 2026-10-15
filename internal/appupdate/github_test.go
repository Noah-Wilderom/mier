package appupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func updateResponse(request *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status, Header: make(http.Header), Request: request,
		Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)),
	}
}

func updateJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func publishedRelease(tag string) githubRelease {
	published := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	return githubRelease{
		Tag: tag, Published: &published, Prerelease: strings.Contains(strings.SplitN(tag, "+", 2)[0], "-"),
		Assets: []githubAsset{{Name: "manifest.json", URL: releaseDownloadURL + tag + "/manifest.json", Size: 1024}},
	}
}

// Exercise the real GitHub adapter and Wails manifest parser without live
// network access. Unknown URLs fail, including accidental /latest requests.
func feedClient(t *testing.T, pages [][]githubRelease, documents map[string][]byte) *http.Client {
	t.Helper()
	wirePages := make([][]byte, len(pages))
	for i, page := range pages {
		wirePages[i] = updateJSON(t, page)
	}
	return &http.Client{Transport: updateTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "api.github.com" && request.URL.Path == "/repos/"+releaseRepository+"/releases" {
			page, err := strconv.Atoi(request.URL.Query().Get("page"))
			if err != nil || page < 1 || request.URL.Query().Get("per_page") != "100" {
				return nil, fmt.Errorf("unexpected release pagination: %s", request.URL)
			}
			if page > len(wirePages) {
				return updateResponse(request, 200, []byte("[]")), nil
			}
			return updateResponse(request, 200, wirePages[page-1]), nil
		}
		if data, ok := documents[request.URL.Scheme+"://"+request.URL.Host+request.URL.Path]; ok {
			return updateResponse(request, 200, data), nil
		}
		return nil, fmt.Errorf("unexpected updater URL: %s", request.URL)
	})}
}

func manifestFixture(t *testing.T, version, channel string) (ed25519.PublicKey, map[string]any, []byte) {
	t.Helper()
	pub, fixture := signedFixture(t)
	artifacts := []map[string]any{}
	for _, platform := range []string{"darwin", "linux", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			filename := "mier-" + version + "-" + platform + "-" + arch + ".bin"
			artifacts = append(artifacts, map[string]any{
				"platform": platform, "arch": arch, "filename": filename, "filetype": "bin",
				"url": releaseDownloadURL + "v" + version + "/" + filename, "size": len(fixture.payload),
				"digestAlgo": "sha512", "digest": fixture.release.Verification.Digest,
				"signatureAlgo": "ed25519ph", "signature": fixture.release.Verification.Signature,
			})
		}
	}
	return pub, map[string]any{
		"schemaVersion": 1, "version": version, "channel": channel, "artifacts": artifacts,
	}, fixture.payload
}

func TestReleaseChannelsRequireFullSemVer(t *testing.T) {
	for _, test := range []struct{ version, channel string }{
		{"v1.2.3", "stable"}, {"1.2.3+build.alpha", "stable"},
		{"1.2.3-alpha", "alpha"}, {"v1.2.3-alpha.10+build.4", "alpha"},
		{"1.2.3-beta.2", "beta"}, {"1.2.3-rc.1", "rc"},
	} {
		t.Run(test.version, func(t *testing.T) {
			pub, _ := signedFixture(t)
			config, err := NewConfig(test.version, pub)
			if err != nil || config.Channel != test.channel || config.CurrentVersion != strings.TrimPrefix(test.version, "v") {
				t.Fatalf("unexpected channel configuration: %+v, %v", config, err)
			}
		})
	}
	for _, version := range []string{"", "dev", "1", "v1.2", "v01.2.3", "1.2.3-alpha.01", "1.2.3-preview.1", "1.2.3-alpha1", "1.2.3-RC.1"} {
		if _, err := releaseChannel(version); err == nil {
			t.Errorf("unsupported release accepted: %q", version)
		}
	}
}

func TestGitHubUpdatesStayOnInstalledChannel(t *testing.T) {
	releases := []githubRelease{}
	for _, tag := range []string{
		"v9.0.0", "v9.0.0-alpha.99", "v9.0.0-beta.99", "v9.0.0-rc.99",
		"v1.2.3-alpha.2", "v1.2.3-alpha.10", "v1.2.3-alpha.9",
		"v1.2.3-beta.3", "v1.2.3-rc.2", "v1.2.3", "v1.2.2", "v1.2.3-nightly.4",
	} {
		release := publishedRelease(tag)
		if strings.HasPrefix(tag, "v9.") {
			release.Assets = nil // A published release whose upload is still in progress.
		}
		releases = append(releases, release)
	}
	for _, test := range []struct{ current, want, channel string }{
		{"1.2.3-alpha.1", "1.2.3-alpha.10", "alpha"},
		{"1.2.3-beta.1", "1.2.3-beta.3", "beta"},
		{"1.2.3-rc.1", "1.2.3-rc.2", "rc"},
		{"1.2.1", "1.2.3", "stable"},
	} {
		t.Run(test.channel, func(t *testing.T) {
			pub, manifest, _ := manifestFixture(t, test.want, test.channel)
			client := feedClient(t, [][]githubRelease{releases}, map[string][]byte{
				releaseDownloadURL + "v" + test.want + "/manifest.json": updateJSON(t, manifest),
			})
			config, err := newConfig(test.current, pub, client)
			if err != nil {
				t.Fatal(err)
			}
			for _, platform := range []string{"darwin", "linux", "windows"} {
				for _, arch := range []string{"amd64", "arm64"} {
					release, err := config.Providers[0].Check(context.Background(), updater.CheckRequest{CurrentVersion: test.current, Platform: platform, Arch: arch})
					if err != nil || release == nil {
						t.Fatalf("no %s/%s update: %v, %v", platform, arch, release, err)
					}
					if release.Version != test.want || release.Channel != test.channel || release.Artifact.Platform != platform || release.Artifact.Arch != arch || !strings.HasSuffix(release.Artifact.Filename, "-"+platform+"-"+arch+".bin") {
						t.Fatalf("wrong update selected: %+v", release)
					}
				}
			}
		})
	}
}

func TestGitHubUpdatesDoNotPromoteDowngradeOrReinstall(t *testing.T) {
	for _, current := range []string{"1.2.3", "1.2.3-alpha.10", "1.2.3-beta.10", "1.2.3-rc.10"} {
		t.Run(current, func(t *testing.T) {
			channel, _ := releaseChannel(current)
			releases := []githubRelease{
				publishedRelease("v" + current), publishedRelease("v" + current + "+build.2"),
				publishedRelease("v1.1.0"), publishedRelease("v1.2.3-alpha.2"),
				publishedRelease("v1.2.3-beta.2"), publishedRelease("v1.2.3-rc.2"),
			}
			for _, tag := range []string{"v9.0.0", "v9.0.0-alpha.1", "v9.0.0-beta.1", "v9.0.0-rc.1"} {
				if other, _ := releaseChannel(tag); other != channel {
					releases = append(releases, publishedRelease(tag))
				}
			}
			provider, _ := newGitHubProvider(channel, feedClient(t, [][]githubRelease{releases}, nil))
			// No manifest is served: checking another channel or an already
			// installed version would fail this check, rather than pass silently.
			release, err := provider.Check(context.Background(), updater.CheckRequest{CurrentVersion: current, Platform: "linux", Arch: "amd64"})
			if err != nil || release != nil {
				t.Fatalf("unexpected update: %+v, %v", release, err)
			}
		})
	}
}

func TestGitHubDiscoveryFiltersUnpublishedAndInconsistentReleases(t *testing.T) {
	valid := publishedRelease("v1.2.3-alpha.2")
	releases := []githubRelease{valid}
	for i, change := range []func(*githubRelease){
		func(r *githubRelease) { r.Draft = true },
		func(r *githubRelease) { r.Published = nil },
		func(r *githubRelease) { r.Prerelease = false },
		func(r *githubRelease) { r.Assets = nil },
		func(r *githubRelease) { r.Assets[0].Size = 0 },
		func(r *githubRelease) { r.Assets[0].URL = "https://example.com/manifest.json" },
	} {
		release := publishedRelease(fmt.Sprintf("v1.2.3-alpha.%d", i+3))
		change(&release)
		releases = append(releases, release)
	}
	provider, _ := newGitHubProvider("alpha", feedClient(t, [][]githubRelease{releases}, nil))
	candidate, err := provider.latest(context.Background(), "1.2.3-alpha.1")
	if err != nil || candidate == nil || candidate.version != "1.2.3-alpha.2" {
		t.Fatalf("incomplete release selected: %+v, %v", candidate, err)
	}
	stable := publishedRelease("v2.0.0")
	stable.Prerelease = true
	provider, _ = newGitHubProvider("stable", feedClient(t, [][]githubRelease{{stable}}, nil))
	if candidate, err := provider.latest(context.Background(), "1.0.0"); err != nil || candidate != nil {
		t.Fatalf("GitHub prerelease entered stable channel: %+v, %v", candidate, err)
	}
}

func TestGitHubDiscoverySortsAcrossPagesBySemVer(t *testing.T) {
	first := make([]githubRelease, 100)
	for i := range first {
		first[i] = publishedRelease(fmt.Sprintf("v0.1.0-alpha.%d", i))
	}
	first[0] = publishedRelease("v2.0.0-alpha.2")
	provider, _ := newGitHubProvider("alpha", feedClient(t, [][]githubRelease{first, {
		publishedRelease("v2.0.0-alpha.10"), publishedRelease("v1.10.0-alpha.99"),
	}}, nil))
	candidate, err := provider.latest(context.Background(), "1.2.0-alpha.1")
	if err != nil || candidate == nil || candidate.version != "2.0.0-alpha.10" {
		t.Fatalf("wrong paginated version: %+v, %v", candidate, err)
	}
}

func TestGitHubManifestMustMatchPublishedTagAndChannel(t *testing.T) {
	for _, test := range []struct {
		name, version, channel string
		accepted               bool
	}{
		{"correct channel", "1.2.3-alpha.2", "alpha", true},
		{"legacy stable label", "1.2.3-alpha.2", "stable", true},
		{"legacy omitted label", "1.2.3-alpha.2", "", true},
		{"another prerelease channel", "1.2.3-alpha.2", "beta", false},
		{"different version", "1.2.3-alpha.3", "alpha", false},
		{"different version channel", "1.2.3-beta.2", "stable", false},
		{"outdated manifest", "1.2.3-alpha.1", "alpha", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			pub, manifest, _ := manifestFixture(t, test.version, test.channel)
			client := feedClient(t, [][]githubRelease{{publishedRelease("v1.2.3-alpha.2")}}, map[string][]byte{
				releaseDownloadURL + "v1.2.3-alpha.2/manifest.json": updateJSON(t, manifest),
			})
			config, err := newConfig("1.2.3-alpha.1", pub, client)
			if err != nil {
				t.Fatal(err)
			}
			release, err := config.Providers[0].Check(context.Background(), updater.CheckRequest{CurrentVersion: config.CurrentVersion, Platform: "linux", Arch: "arm64"})
			if test.accepted {
				if err != nil || release == nil || release.Channel != "alpha" {
					t.Fatalf("valid signed manifest rejected: %+v, %v", release, err)
				}
			} else if err == nil || release != nil {
				t.Fatalf("inconsistent manifest accepted: %+v, %v", release, err)
			}
		})
	}
}

func TestGitHubUpdateStagesOnlyPinnedSignedPayload(t *testing.T) {
	for _, mode := range []string{"valid", "unsigned", "another signing key", "altered download"} {
		t.Run(mode, func(t *testing.T) {
			pub, manifest, payload := manifestFixture(t, "1.2.3-rc.2", "rc")
			if mode == "unsigned" {
				for _, artifact := range manifest["artifacts"].([]map[string]any) {
					delete(artifact, "signature")
				}
			} else if mode == "another signing key" {
				pub, _ = signedFixture(t)
			}
			if mode == "altered download" {
				payload = []byte("untrusted replacement")
			}
			documents := map[string][]byte{
				releaseDownloadURL + "v1.2.3-rc.2/manifest.json": updateJSON(t, manifest),
			}
			for _, artifact := range manifest["artifacts"].([]map[string]any) {
				documents[artifact["url"].(string)] = payload
			}
			client := feedClient(t, [][]githubRelease{{publishedRelease("v1.2.3-rc.2")}}, documents)
			config, err := newConfig("1.2.3-rc.1", pub, client)
			if err != nil {
				t.Fatal(err)
			}
			config.Window = updater.WindowNone
			config.CheckInterval = 0
			appUpdater := updater.New(headlessHost{})
			if err := appUpdater.Init(config); err != nil {
				t.Fatal(err)
			}
			release, err := appUpdater.Check(context.Background())
			if mode == "unsigned" || mode == "another signing key" {
				if err == nil || release != nil {
					t.Fatalf("untrusted manifest accepted: %+v, %v", release, err)
				}
				return
			}
			if err != nil || release == nil {
				t.Fatalf("valid manifest rejected: %+v, %v", release, err)
			}
			err = appUpdater.DownloadAndInstall(context.Background())
			if mode == "altered download" {
				if err == nil || !strings.Contains(err.Error(), "digest mismatch") || appUpdater.State() == updater.StateReady {
					t.Fatalf("altered download staged: state=%s err=%v", appUpdater.State(), err)
				}
				return
			}
			if err != nil || appUpdater.State() != updater.StateReady {
				t.Fatalf("signed update not staged: state=%s err=%v", appUpdater.State(), err)
			}
			staged := appUpdater.DownloadedPath()
			t.Cleanup(func() { _ = os.Remove(staged) })
			got, err := os.ReadFile(staged)
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("staged update changed: %q, %v", got, err)
			}
		})
	}
}

func TestGitHubDiscoveryReportsErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"rate limited", 403, `{"message":"API rate limit exceeded"}`},
		{"server unavailable", 503, "unavailable"},
		{"invalid JSON", 200, "invalid"},
		{"null release list", 200, "null"},
		{"oversized release list", 200, strings.Repeat(" ", maxReleasePageBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: updateTransport(func(request *http.Request) (*http.Response, error) {
				return updateResponse(request, test.status, []byte(test.body)), nil
			})}
			provider, _ := newGitHubProvider("alpha", client)
			if release, err := provider.Check(context.Background(), updater.CheckRequest{CurrentVersion: "1.2.3-alpha.1"}); err == nil || release != nil {
				t.Fatalf("lookup error hidden: %+v, %v", release, err)
			}
		})
	}
	client := &http.Client{Transport: updateTransport(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})}
	provider, _ := newGitHubProvider("alpha", client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Check(ctx, updater.CheckRequest{CurrentVersion: "1.2.3-alpha.1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestPublishedManifestFailuresAreNotReportedAsUpToDate(t *testing.T) {
	for _, status := range []int{204, 404, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			client := &http.Client{Transport: updateTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host == "api.github.com" {
					return updateResponse(request, 200, updateJSON(t, []githubRelease{publishedRelease("v1.2.3-beta.2")})), nil
				}
				return updateResponse(request, status, nil), nil
			})}
			provider, _ := newGitHubProvider("beta", client)
			if release, err := provider.Check(context.Background(), updater.CheckRequest{CurrentVersion: "1.2.3-beta.1"}); err == nil || release != nil {
				t.Fatalf("published manifest failure hidden: %+v, %v", release, err)
			}
		})
	}
}

func TestManifestURLIsBoundToRepositoryAndTag(t *testing.T) {
	const tag = "v1.2.3-beta.2+build.1"
	valid := releaseDownloadURL + tag + "/manifest.json"
	if !validManifestURL(valid, tag) || !validManifestURL(strings.ReplaceAll(valid, "+", "%2B"), tag) {
		t.Fatal("valid GitHub manifest rejected")
	}
	for _, invalid := range []string{
		strings.Replace(valid, "https://", "http://", 1),
		strings.Replace(valid, "github.com", "github.com.evil.example", 1),
		strings.Replace(valid, "github.com", "user@github.com", 1),
		strings.Replace(valid, releaseRepository, "other/repository", 1),
		strings.Replace(valid, tag, "v1.2.3-beta.1", 1),
		valid + "?token=other", valid + "#fragment",
	} {
		if validManifestURL(invalid, tag) {
			t.Errorf("unrelated manifest accepted: %s", invalid)
		}
	}
}
