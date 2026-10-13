package appupdate

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

func TestSelfUpdatesRespectLinuxPackageOwnership(t *testing.T) {
	for _, test := range []struct {
		name, goos, executable, appImage string
		want                             bool
	}{
		{"pacman apt and dnf", "linux", "/usr/bin/mag-ik-een-review", "", false},
		{"system local installation", "linux", "/usr/local/bin/mag-ik-een-review", "", false},
		{"opt installation", "linux", "/opt/mier/mag-ik-een-review", "", false},
		{"normalized system path", "linux", "/tmp/../usr/bin/mag-ik-een-review", "", false},
		{"AppImage mount", "linux", "/tmp/.mount_mier/usr/bin/mag-ik-een-review", "/home/user/MIER.AppImage", false},
		{"portable user binary", "linux", "/home/user/.local/bin/mag-ik-een-review", "", true},
		{"unknown executable", "linux", "", "", false},
		{"macOS app bundle", "darwin", "/Applications/MIER.app/Contents/MacOS/mag-ik-een-review", "", true},
		{"per-user Windows setup", "windows", `C:\Users\user\AppData\Local\Programs\MIER\mag-ik-een-review.exe`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := SupportsSelfUpdate(test.goos, test.executable, test.appImage); got != test.want {
				t.Fatalf("SupportsSelfUpdate = %v, want %v", got, test.want)
			}
		})
	}
}

type fixtureProvider struct {
	release *updater.Release
	payload []byte
	err     error
}

func (p fixtureProvider) Name() string { return "fixture" }
func (p fixtureProvider) Check(context.Context, updater.CheckRequest) (*updater.Release, error) {
	return p.release, p.err
}
func (p fixtureProvider) Download(_ context.Context, _ *updater.Release, dst io.Writer, progress func(int64, int64)) error {
	_, err := dst.Write(p.payload)
	return err
}

func signedFixture(t *testing.T) (ed25519.PublicKey, fixtureProvider) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("verified release binary")
	digest := sha512.Sum512(payload)
	signature, err := priv.Sign(rand.Reader, digest[:], &ed25519.Options{Hash: crypto.SHA512})
	if err != nil {
		t.Fatal(err)
	}
	return pub, fixtureProvider{
		payload: payload,
		release: &updater.Release{
			Version:  "1.1.0",
			Artifact: updater.Artifact{Filename: "mier-linux-amd64", Filetype: "bin", Size: int64(len(payload))},
			Verification: &updater.Verification{
				DigestAlgo: "sha512", Digest: digest[:], SignatureAlgo: "ed25519ph", Signature: signature,
			},
		},
	}
}

func TestSignedProviderRejectsUnauthenticatedReleases(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*updater.Release)
	}{
		{"unsigned", func(r *updater.Release) { r.Verification = nil }},
		{"checksum only", func(r *updater.Release) { r.Verification.Signature = nil }},
		{"missing algorithm", func(r *updater.Release) { r.Verification.SignatureAlgo = "" }},
		{"other digest", func(r *updater.Release) { r.Verification.DigestAlgo = "sha256" }},
		{"truncated digest", func(r *updater.Release) { r.Verification.Digest = r.Verification.Digest[:32] }},
		{"altered digest", func(r *updater.Release) { r.Verification.Digest[0] ^= 1 }},
		{"altered signature", func(r *updater.Release) { r.Verification.Signature[0] ^= 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			pub, fixture := signedFixture(t)
			test.change(fixture.release)
			provider := signedProvider{Provider: fixture, key: pub}
			if release, err := provider.Check(context.Background(), updater.CheckRequest{}); err == nil || release != nil {
				t.Fatalf("untrusted release accepted: release=%v err=%v", release, err)
			}
		})
	}
}

func TestSignedProviderRejectsAnotherSigningKey(t *testing.T) {
	_, fixture := signedFixture(t)
	otherKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	provider := signedProvider{Provider: fixture, key: otherKey}
	if release, err := provider.Check(context.Background(), updater.CheckRequest{}); err == nil || release != nil {
		t.Fatalf("release signed by another key was accepted: %v, %v", release, err)
	}
}

func TestSignedProviderPreservesNoUpdateAndNetworkErrors(t *testing.T) {
	pub, fixture := signedFixture(t)
	fixture.release = nil
	provider := signedProvider{Provider: fixture, key: pub}
	if release, err := provider.Check(context.Background(), updater.CheckRequest{}); release != nil || err != nil {
		t.Fatalf("no release: %v, %v", release, err)
	}
	want := errors.New("network unavailable")
	fixture.err = want
	provider.Provider = fixture
	if _, err := provider.Check(context.Background(), updater.CheckRequest{}); !errors.Is(err, want) {
		t.Fatalf("network error lost: %v", err)
	}
}

type headlessHost struct{}

func (headlessHost) Emit(string, ...any) bool         { return true }
func (headlessHost) OnEvent(string, func(any)) func() { return func() {} }
func (headlessHost) OpenWindow(updater.WindowOptions) updater.WindowHandle {
	panic("unexpected update window")
}
func (headlessHost) Quit() { panic("tests must not restart the installed application") }

func TestWailsStagesOnlyVerifiedDownloads(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		name := "signed bytes"
		if tamper {
			name = "altered download"
		}
		t.Run(name, func(t *testing.T) {
			pub, fixture := signedFixture(t)
			if tamper {
				fixture.payload = []byte("untrusted replacement")
			}
			appUpdater := updater.New(headlessHost{})
			if err := appUpdater.Init(updater.Config{
				CurrentVersion: "1.0.0", PublicKey: pub, Window: updater.WindowNone,
				Providers: []updater.Provider{&signedProvider{Provider: fixture, key: pub}},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := appUpdater.Check(context.Background()); err != nil {
				t.Fatal(err)
			}
			err := appUpdater.DownloadAndInstall(context.Background())
			if tamper {
				if err == nil || !strings.Contains(err.Error(), "digest mismatch") || appUpdater.State() == updater.StateReady {
					t.Fatalf("altered download was not rejected: state=%s err=%v", appUpdater.State(), err)
				}
				return
			}
			if err != nil || appUpdater.State() != updater.StateReady {
				t.Fatalf("signed download not staged: state=%s err=%v", appUpdater.State(), err)
			}
			staged := appUpdater.DownloadedPath()
			t.Cleanup(func() { _ = os.Remove(staged) })
			got, err := os.ReadFile(staged)
			if err != nil || !bytes.Equal(got, fixture.payload) {
				t.Fatalf("staged bytes changed: %q, %v", got, err)
			}
		})
	}
}

func TestConfigRequiresReleaseVersionAndPinnedPublicKey(t *testing.T) {
	pub, _ := signedFixture(t)
	for _, version := range []string{"", "dev", "not-a-release"} {
		if _, err := NewConfig(version, pub); err == nil {
			t.Fatalf("invalid release version accepted: %q", version)
		}
	}
	if _, err := NewConfig("1.0.0", nil); err == nil {
		t.Fatal("missing public key accepted")
	}
	if _, err := NewConfig("1.0.0", []byte("invalid key")); err == nil {
		t.Fatal("invalid public key accepted")
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	config, err := NewConfig("v1.0.0", pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	if err != nil || config.CurrentVersion != "1.0.0" || !bytes.Equal(config.PublicKey, pub) || config.CheckInterval <= 0 {
		t.Fatalf("invalid release configuration: %+v, %v", config, err)
	}
}
