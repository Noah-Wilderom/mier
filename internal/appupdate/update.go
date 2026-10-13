// Package appupdate configures signed updates published by M.I.E.R.'s release workflow.
package appupdate

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha512"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"
	"golang.org/x/mod/semver"
)

const ManifestURL = "https://github.com/Noah-Wilderom/mier/releases/latest/download/manifest.json"

// Package managers own system installations. AppImages run the executable
// inside a read-only mount, which Wails' binary replacement cannot update.
func SupportsSelfUpdate(goos, executable, appImage string) bool {
	if goos != "linux" {
		return true
	}
	executable = path.Clean(executable)
	return appImage == "" && path.IsAbs(executable) &&
		!strings.HasPrefix(executable, "/usr/") && !strings.HasPrefix(executable, "/opt/")
}

func NewConfig(version string, key []byte) (updater.Config, error) {
	version = strings.TrimPrefix(version, "v")
	if !semver.IsValid("v" + version) {
		return updater.Config{}, errors.New("updates require a release version")
	}
	publicKey, err := parsePublicKey(key)
	if err != nil {
		return updater.Config{}, err
	}
	provider, err := endpoint.New(endpoint.Config{URL: ManifestURL, Channel: "stable"})
	if err != nil {
		return updater.Config{}, err
	}
	return updater.Config{
		CurrentVersion: version,
		PublicKey:      publicKey,
		Providers:      []updater.Provider{&signedProvider{Provider: provider, key: publicKey}},
		CheckInterval:  6 * time.Hour,
	}, nil
}

func parsePublicKey(key []byte) (ed25519.PublicKey, error) {
	if len(key) == ed25519.PublicKeySize {
		return ed25519.PublicKey(key), nil
	}
	block, _ := pem.Decode(key)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("updater signing public key is missing or invalid")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse updater signing public key: %w", err)
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("updater signing key must be Ed25519")
	}
	return publicKey, nil
}

// Wails permits checksum-only feeds. This app accepts only artifacts signed
// with the pinned key; the framework also verifies the downloaded bytes before
// extraction, staging, and its helper-mode replacement of the installed app.
type signedProvider struct {
	updater.Provider
	key ed25519.PublicKey
}

func (p *signedProvider) Check(ctx context.Context, request updater.CheckRequest) (*updater.Release, error) {
	release, err := p.Provider.Check(ctx, request)
	if err != nil || release == nil {
		return release, err
	}
	v := release.Verification
	if v == nil || v.DigestAlgo != "sha512" || len(v.Digest) != sha512.Size || v.SignatureAlgo != "ed25519ph" || len(v.Signature) != ed25519.SignatureSize {
		return nil, errors.New("update is missing a valid signed checksum")
	}
	if err := ed25519.VerifyWithOptions(p.key, v.Digest, v.Signature, &ed25519.Options{Hash: crypto.SHA512}); err != nil {
		return nil, fmt.Errorf("verify update signature: %w", err)
	}
	return release, nil
}
