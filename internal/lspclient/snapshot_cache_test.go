package lspclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAnalysisSnapshotCacheSurvivesRestartAndInvalidatesSource(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join(directory, "repo")
	os.Mkdir(root, 0700)
	root, _ = filepath.EvalSymlinks(root)
	writeTestFile(t, filepath.Join(root, "composer.json"), "{}")
	dependency := filepath.Join(root, "vendor", "package", "item.php")
	writeTestFile(t, dependency, "<?php class Before {}")
	options := Options{ConfigPath: filepath.Join(directory, "settings.json"), CacheDirectory: filepath.Join(directory, "cache")}
	archive := ArchiveResult{Revision: testRevision, Tar: sourceArchive(t, map[string]string{"composer.json": "{}", "use.php": "<?php use Original;"})}
	request := requestFor(root, "use.php")
	capture := func() (*Manager, *workspace) {
		m := New(options)
		m.cacheDirectory = t.TempDir()
		w, err := m.captureWorkspace(context.Background(), request, archive, "vendor")
		if err != nil {
			t.Fatal(err)
		}
		return m, w
	}
	m, first := capture()
	path := first.directory
	first.close()
	m.Close()
	m, second := capture()
	if second.directory != path {
		t.Fatal("restart did not reuse validated snapshot")
	}
	status, _ := m.AnalysisStatus(context.Background(), request)
	if !status.Cached {
		t.Fatal("cache reuse missing in progress status")
	}
	second.close()
	m.Close()
	info, _ := os.Stat(dependency)
	writeTestFile(t, dependency, "<?php class After_ {}")
	os.Chtimes(dependency, info.ModTime(), info.ModTime())
	m, third := capture()
	if third.directory == path {
		t.Fatal("dependency content changed with preserved mtime but cache was reused")
	}
	third.close()
	m.Close()
	// A server modifying captured source must never contaminate the next review.
	writeTestFile(t, filepath.Join(path, "use.php"), "<?php DIRTY;")
	writeTestFile(t, dependency, "<?php class Before {}")
	m, fourth := capture()
	defer m.Close()
	defer fourth.close()
	text, _, _, err := fourth.read(filepath.Join(fourth.directory, "use.php"))
	if err != nil || text != "<?php use Original;" {
		t.Fatalf("corrupted source reused: %q %v", text, err)
	}
	// A second process cannot reuse a snapshot still owned by a live server.
	another, other := capture()
	defer another.Close()
	defer other.close()
	if other.directory == fourth.directory {
		t.Fatal("concurrent analysis processes share a writable workspace")
	}
}
