package gitclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// DiffText builds standard Git hunks for providers which expose blobs but no
// unified-diff endpoint. It never writes into the user's checkout or index.
func (c *Client) DiffText(ctx context.Context, before, after string) (string, error) {
	for _, text := range []string{before, after} {
		if len(text) > 2*1024*1024 {
			return "", errors.New("Source content exceeds the 2 MiB comparison limit")
		}
		if strings.ContainsRune(text, 0) || !utf8.ValidString(text) {
			return "", errors.New("Binary content has no text diff")
		}
	}
	dir, err := os.MkdirTemp("", "mier-provider-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err = os.WriteFile(filepath.Join(dir, "before"), []byte(before), 0600); err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(dir, "after"), []byte(after), 0600); err != nil {
		return "", err
	}
	out, err := c.executeInputResult(ctx, dir, c.options.LocalTimeout, nil, nil, nil, "-c", "core.autocrlf=false", "-c", "diff.suppressBlankEmpty=false", "diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--unified=3", "--output-indicator-new=+", "--output-indicator-old=-", "--output-indicator-context= ", "--", "before", "after")
	if err != nil && !isExitCode(err, 1) {
		return "", err
	}
	patch := string(out)
	start := strings.Index(patch, "\n@@ ")
	if start < 0 {
		return "", nil
	}
	return patch[start+1:], nil
}
