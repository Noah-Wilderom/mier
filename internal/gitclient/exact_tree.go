package gitclient

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
)

type archiveEntry struct{ data []byte }
type treeBlob struct{ oid, mode string }

func (c *Client) treeEntries(ctx context.Context, root, revision string) (map[string]archiveEntry, error) {
	listing, err := c.run(ctx, root, c.options.LocalTimeout, "ls-tree", "--full-tree", "-r", "-z", revision)
	if err != nil {
		return nil, err
	}
	blobs := map[string]treeBlob{}
	for _, record := range strings.Split(string(listing), "\x00") {
		if record == "" {
			continue
		}
		metadata, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 3 || !fullCommitID.MatchString(fields[2]) {
			return nil, errors.New("Git returned an invalid immutable tree entry")
		}
		if fields[1] == "blob" && (fields[0] == "100644" || fields[0] == "100755") {
			blobs[name] = treeBlob{oid: fields[2], mode: fields[0]}
		}
	}
	return c.blobEntries(ctx, root, blobs)
}

// Blob IDs are read from one tree/index listing, then resolved in a bounded
// batch. Paths never enter the cat-file protocol and export attributes never
// omit or rewrite the selected source bytes.
func (c *Client) blobEntries(ctx context.Context, root string, blobs map[string]treeBlob) (map[string]archiveEntry, error) {
	if len(blobs) > 20000 {
		return nil, errors.New("Source exceeds the 20,000-file analysis limit")
	}
	names := []string{}
	for name, blob := range blobs {
		if blob.mode == "100644" || blob.mode == "100755" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	entries := map[string]archiveEntry{}
	if len(names) == 0 {
		return entries, nil
	}
	var input strings.Builder
	for _, name := range names {
		if !fullCommitID.MatchString(blobs[name].oid) {
			return nil, errors.New("Git returned an invalid source blob ID")
		}
		input.WriteString(blobs[name].oid + "\n")
	}
	reader := &Client{gitPath: c.gitPath, options: c.options}
	reader.options.MaxOutputBytes = 72 * 1024 * 1024
	metadata, err := reader.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, []byte(input.String()), "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(metadata), "\n"), "\n")
	if len(lines) != len(names) {
		return nil, errors.New("Git returned an incomplete source blob listing")
	}
	sizes := map[string]int64{}
	eligible := []string{}
	var total int64
	input.Reset()
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != blobs[names[index]].oid || fields[1] != "blob" {
			return nil, errors.New("Git source object is not a blob")
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return nil, errors.New("Git returned an invalid source blob size")
		}
		if size > 4*1024*1024 {
			continue
		}
		total += size
		if total > 64*1024*1024 {
			return nil, errors.New("Source exceeds the 64 MiB analysis limit")
		}
		eligible = append(eligible, names[index])
		sizes[names[index]] = size
		input.WriteString(fields[0] + "\n")
	}
	if len(eligible) == 0 {
		return entries, nil
	}
	data, err := reader.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, []byte(input.String()), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	batch := bufio.NewReader(bytes.NewReader(data))
	for _, name := range eligible {
		line, err := batch.ReadString('\n')
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != blobs[name].oid || fields[1] != "blob" || fields[2] != strconv.FormatInt(sizes[name], 10) {
			return nil, errors.New("Git returned an unexpected source blob")
		}
		body := make([]byte, int(sizes[name]))
		if _, err = io.ReadFull(batch, body); err != nil {
			return nil, err
		}
		end, err := batch.ReadByte()
		if err != nil || end != '\n' {
			return nil, errors.New("Git returned an incomplete source blob")
		}
		entries[name] = archiveEntry{data: body}
	}
	return entries, nil
}
func entriesArchive(ctx context.Context, entries map[string]archiveEntry) ([]byte, error) {
	if len(entries) > 20000 {
		return nil, errors.New("Source exceeds the 20,000-file analysis limit")
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	var total int64
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := entries[name]
		total += int64(len(entry.data))
		if total > 64*1024*1024 {
			return nil, errors.New("Source exceeds the 64 MiB analysis limit")
		}
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(entry.data))}); err != nil {
			return nil, err
		}
		if _, err := writer.Write(entry.data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}
