package lspclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This opt-in test exercises the same downloads, integrity checks, npm command,
// version verification, and activation used by the desktop Install button.
func TestRealManagedToolInstallation(t *testing.T) {
	if os.Getenv("MIER_LSP_INSTALL_SMOKE") != "1" {
		t.Skip("Set MIER_LSP_INSTALL_SMOKE=1 to install and verify official language tools outside the repository")
	}
	directory := os.Getenv("MIER_LSP_INSTALL_TOOLS")
	if directory == "" {
		directory = t.TempDir()
	}
	settings := t.TempDir()
	m := New(Options{ConfigPath: filepath.Join(settings, "language-servers.json"), CacheDirectory: filepath.Join(settings, "cache"), ToolsDirectory: directory})
	defer m.Close()
	languages := []string{"php", "typescript", "vue"}
	if selected := os.Getenv("MIER_LSP_INSTALL_LANGUAGES"); selected != "" {
		languages = strings.Split(selected, ",")
	}
	for _, language := range languages {
		if err := m.InstallServer(context.Background(), language); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { m.tools.workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(6 * time.Minute):
			t.Fatalf("%s installation timed out", language)
		}
		tool := m.tools.statuses(context.Background())[language]
		server := m.detectServer("", language)
		if tool.InstallError != "" || !tool.Ready || !server.Available || server.Source != "managed" {
			t.Fatalf("%s managed installation is unavailable: %#v / %#v", language, tool, server)
		}
		t.Logf("%s %s installed, verified, and detected at %s %v", language, tool.InstalledVersion, server.Command, server.Args)
	}
}

// Start each installed catalog server through the real read-only LSP adapter.
// Installation alone cannot catch a wrong stdio flag or missing runtime import.
func TestRealCatalogServerStartup(t *testing.T) {
	directory := os.Getenv("MIER_LSP_CATALOG_SMOKE")
	if directory == "" {
		t.Skip("Set MIER_LSP_CATALOG_SMOKE to a managed tools directory to start its catalog servers")
	}
	fixtures := map[string]struct{ path, text string }{
		"systemverilog":   {"example.sv", "module example(input logic clk);\nendmodule\n"},
		"dockerfile":      {"Dockerfile", "FROM node:22\nWORKDIR /app\n"},
		"docker-compose":  {"compose.yaml", "services:\n  app:\n    image: nginx\n"},
		"yaml":            {"settings.yaml", "name: example\n"},
		"github-actions":  {".github/workflows/ci.yml", "name: CI\non: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n"},
		"json":            {"settings.json", "{\"name\":\"example\"}\n"},
		"html":            {"index.html", "<!doctype html><html><body><p>Example</p></body></html>\n"},
		"css":             {"style.css", "body { color: red; }\n"},
		"python":          {"main.py", "value: int = 42\nprint(value)\n"},
		"bash":            {"main.sh", "echo hello\n"},
		"astro":           {"index.astro", "---\nconst name = 'World'\n---\n<h1>Hello {name}</h1>\n"},
		"svelte":          {"App.svelte", "<script>let name = 'World'</script><h1>Hello {name}</h1>\n"},
		"graphql":         {"schema.graphql", "type Query { hello: String }\n"},
		"prisma":          {"schema.prisma", "model User {\n  id Int @id\n}\n"},
		"sql":             {"query.sql", "SELECT 1;\n"},
		"cucumber":        {"test.feature", "Feature: Example\n  Scenario: Test\n    Given an example\n"},
		"mdx":             {"README.mdx", "# Example\n\nHello world\n"},
		"solidity":        {"Example.sol", "pragma solidity ^0.8.0;\ncontract Example {}\n"},
		"vim":             {"init.vim", "let g:example = 1\n"},
		"awk":             {"main.awk", "BEGIN { print 1 }\n"},
		"perl":            {"main.pl", "my $example = 1;\n"},
		"twig":            {"index.twig", "<p>{{ name }}</p>\n"},
		"marko":           {"index.marko", "<h1>Hello</h1>\n"},
		"coffeescript":    {"main.coffee", "square = (x) -> x * x\n"},
		"ansible":         {"playbooks/main.yml", "- hosts: all\n  tasks:\n    - name: Example\n      ansible.builtin.debug:\n        msg: hello\n"},
		"azure-pipelines": {"azure-pipelines.yml", "steps:\n  - script: echo hello\n"},
		"turtle":          {"example.ttl", "@prefix ex: <http://example.org/> .\nex:a ex:b ex:c .\n"},
		"go":              {"main.go", "package main\nvar value int = 42\nfunc main() { println(value) }\n"},
		"jsonnet":         {"main.jsonnet", "local value = 42;\n{ result: value }\n"},
		"jq":              {"main.jq", ".example\n"},
		"cue":             {"main.cue", "package example\nvalue: 42\n"},
		"rego":            {"main.rego", "package example\ndefault allow := false\n"},
		"helm":            {"templates/deployment.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Chart.Name }}\n"},
		"terraform":       {"main.tf", "variable \"name\" {\n  type = string\n}\n"},
		"cmake":           {"CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\nproject(example)\n"},
		"fortran":         {"main.f90", "program example\n  print *, 'Hello'\nend program example\n"},
		"nginx":           {"nginx.conf", "events {}\nhttp { server { listen 80; } }\n"},
		"robot":           {"example.robot", "*** Test Cases ***\nExample\n    Log    Hello\n"},
	}
	for _, definition := range serverCatalog {
		fixture, ok := fixtures[definition.ID]
		if !ok {
			continue
		}
		t.Run(definition.ID, func(t *testing.T) {
			settings, root := t.TempDir(), t.TempDir()
			files := map[string]string{fixture.path: fixture.text}
			if definition.ID == "go" {
				files["go.mod"] = "module example.test/catalog\n\ngo 1.24.0\n"
			}
			if definition.ID == "helm" {
				files["Chart.yaml"] = "apiVersion: v2\nname: example\nversion: 0.1.0\n"
			}
			if definition.ID == "cue" {
				files["cue.mod/module.cue"] = "module: \"example.test/catalog@v0\"\nlanguage: version: \"v0.17.0\"\n"
			}
			archive := sourceArchive(t, files)
			m := New(Options{ConfigPath: filepath.Join(settings, "settings.json"), CacheDirectory: filepath.Join(settings, "cache"), ToolsDirectory: directory,
				Archive: func(_ context.Context, request CodePositionRequest) (ArchiveResult, error) {
					return ArchiveResult{Revision: request.Revision, Tar: archive}, nil
				}})
			defer m.Close()
			if _, ready := m.tools.configuration(definition.ID); !ready {
				t.Skip("This server has not been installed in the smoke directory")
			}
			request := requestFor(root, fixture.path)
			request.Character = 0
			if err := m.Warmup(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if definition.ID == "dockerfile" || definition.ID == "python" || definition.ID == "go" {
				request.Character = 2
				if definition.ID == "python" {
					request.Line, request.Character = 1, 7
				}
				if definition.ID == "go" {
					request.Line, request.Character = 1, 5
				}
				hover, err := m.Hover(context.Background(), request)
				if err != nil || hover.Contents == "" {
					t.Fatalf("Real symbol hover failed: %#v / %v", hover, err)
				}
				t.Logf("Real hover: %.120s", hover.Contents)
			}
		})
	}
}
