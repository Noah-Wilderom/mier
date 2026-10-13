package gitclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ArchiveWorkspace reads one coherent stage-0 index listing or all actual
// working files into an immutable content-addressed analysis revision. Blob IDs
// from the captured index are never resolved through a later mutable :0:path.
func (c *Client) ArchiveWorkspace(ctx context.Context, path, layer string) (CommitArchiveResult, error) {
	if layer != "working" && layer != "index" {
		return CommitArchiveResult{}, errors.New("Choose the working tree or Git index source layer")
	}
	var result CommitArchiveResult
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		listing, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--stage", "-z")
		if err != nil {
			return err
		}
		blobs := map[string]treeBlob{}
		names := map[string]bool{}
		conflicts := false
		for _, record := range strings.Split(string(listing), "\x00") {
			if record == "" {
				continue
			}
			metadata, name, ok := strings.Cut(record, "\t")
			fields := strings.Fields(metadata)
			if !ok || len(fields) != 3 || !fullCommitID.MatchString(fields[1]) {
				return errors.New("Git returned an invalid index entry")
			}
			names[name] = true
			if fields[2] == "0" {
				blobs[name] = treeBlob{oid: fields[1], mode: fields[0]}
			} else {
				conflicts = true
			}
		}
		var entries map[string]archiveEntry
		if layer == "index" {
			if conflicts {
				return errors.New("Resolve Git index conflicts before inspecting index symbols")
			}
			entries, err = c.blobEntries(ctx, root, blobs)
			if err != nil {
				return err
			}
		} else {
			untracked, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--others", "--exclude-standard", "-z")
			if err != nil {
				return err
			}
			for _, name := range strings.Split(string(untracked), "\x00") {
				if name != "" {
					names[name] = true
				}
			}
			if len(names) > 20000 {
				return errors.New("Working source exceeds the 20,000-file analysis limit")
			}
			flags, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "-v", "-z")
			if err != nil {
				return err
			}
			sparse := map[string]bool{}
			for _, record := range strings.Split(string(flags), "\x00") {
				if len(record) > 2 && strings.ToUpper(record[:1]) == "S" {
					sparse[record[2:]] = true
				}
			}
			entries = map[string]archiveEntry{}
			fallback := map[string]treeBlob{}
			observed := map[string]os.FileInfo{}
			confined, err := os.OpenRoot(root)
			if err != nil {
				return err
			}
			defer confined.Close()
			var total int64
			for raw := range names {
				if err := ctx.Err(); err != nil {
					return err
				}
				name, err := validateFile(root, raw)
				if err != nil {
					return err
				}
				info, err := confined.Lstat(name)
				if os.IsNotExist(err) {
					if sparse[name] {
						if blob, ok := blobs[name]; ok {
							fallback[name] = blob
						}
					}
					continue
				}
				if err != nil {
					return fmt.Errorf("Read working file metadata: %w", err)
				}
				if !info.Mode().IsRegular() || info.Size() > 4*1024*1024 {
					continue
				}
				total += info.Size()
				if total > 64*1024*1024 {
					return errors.New("Working source exceeds the 64 MiB analysis limit")
				}
				file, err := confined.Open(name)
				if err != nil {
					return fmt.Errorf("Read working source without external links: %w", err)
				}
				before, err := file.Stat()
				if err != nil {
					file.Close()
					return err
				}
				if !os.SameFile(info, before) {
					file.Close()
					return errors.New("Working source changed while its snapshot was captured. Try again")
				}
				body, err := io.ReadAll(io.LimitReader(file, 4*1024*1024+1))
				after, statErr := file.Stat()
				file.Close()
				if err != nil {
					return err
				}
				if statErr != nil {
					return statErr
				}
				if len(body) > 4*1024*1024 || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
					return errors.New("Working source changed while its snapshot was captured. Try again")
				}
				observed[name] = after
				entries[name] = archiveEntry{data: body}
			}
			// Detect cross-file edits and newly replaced paths before returning the
			// captured tree, rather than mix file versions as a user edits externally.
			for name, before := range observed {
				after, err := confined.Lstat(name)
				if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
					return errors.New("Working source changed while its snapshot was captured. Try again")
				}
			}
			if len(fallback) > 0 {
				missing, err := c.blobEntries(ctx, root, fallback)
				if err != nil {
					return err
				}
				for name, entry := range missing {
					entries[name] = entry
				}
			}
			currentIndex, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--stage", "-z")
			if err != nil {
				return err
			}
			currentUntracked, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--others", "--exclude-standard", "-z")
			if err != nil {
				return err
			}
			if string(currentIndex) != string(listing) || string(currentUntracked) != string(untracked) {
				return errors.New("Local file/index selection changed while its snapshot was captured. Try again")
			}
		}
		data, err := entriesArchive(ctx, entries)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		result = CommitArchiveResult{Revision: hex.EncodeToString(digest[:]), Data: data}
		return nil
	})
	return result, err
}
