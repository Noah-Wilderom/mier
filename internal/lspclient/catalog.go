package lspclient

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed catalog.json
var catalogJSON []byte

type FileAssociation struct {
	Pattern    string `json:"pattern"`
	LanguageID string `json:"languageId"`
}
type serverInstallation struct {
	Method       string            `json:"method"`
	Package      string            `json:"package,omitempty"`
	Module       string            `json:"module,omitempty"`
	Binary       string            `json:"binary,omitempty"`
	Requires     string            `json:"requires"`
	CGO          bool              `json:"cgo,omitempty"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
}
type serverDefinition struct {
	ID                    string             `json:"id"`
	Name                  string             `json:"name"`
	Label                 string             `json:"label"`
	Category              string             `json:"category"`
	Description           string             `json:"description"`
	Files                 []FileAssociation  `json:"files"`
	Program               string             `json:"program"`
	Args                  []string           `json:"args"`
	Homepage              string             `json:"homepage"`
	Install               serverInstallation `json:"install"`
	Settings              map[string]any     `json:"settings,omitempty"`
	InitializationOptions map[string]any     `json:"initializationOptions,omitempty"`
}

var serverCatalog = loadServerCatalog()
var catalogByID = indexServerCatalog()
var languageNames, setupURLs = catalogNames()
var catalogPatterns = compileCatalogPatterns()

func loadServerCatalog() []serverDefinition {
	var result []serverDefinition
	decoder := json.NewDecoder(bytes.NewReader(catalogJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		panic(fmt.Errorf("invalid language-server catalog: %w", err))
	}
	return result
}
func indexServerCatalog() map[string]*serverDefinition {
	result := make(map[string]*serverDefinition, len(serverCatalog))
	for index := range serverCatalog {
		definition := &serverCatalog[index]
		if result[definition.ID] != nil || definition.ID == "" {
			panic("duplicate or missing language-server catalog ID")
		}
		result[definition.ID] = definition
	}
	return result
}
func catalogNames() (map[string]string, map[string]string) {
	names, urls := map[string]string{}, map[string]string{}
	for _, server := range serverCatalog {
		names[server.ID], urls[server.ID] = server.Name, server.Homepage
	}
	return names, urls
}
func managedLanguages() []string {
	var result []string
	for _, server := range serverCatalog {
		if server.Install.Method != "external" {
			result = append(result, server.ID)
		}
	}
	return result
}

// A whole **/ segment matches zero or more directories; * and ? stay within
// their segment. Keep this grammar in sync with languageServerCatalog.ts.
func compileCatalogPatterns() map[string]*regexp.Regexp {
	result := map[string]*regexp.Regexp{}
	for _, server := range serverCatalog {
		for _, file := range server.Files {
			pattern := strings.ToLower(file.Pattern)
			var expression strings.Builder
			expression.WriteString("^")
			for index := 0; index < len(pattern); index++ {
				switch {
				case strings.HasPrefix(pattern[index:], "**/"):
					expression.WriteString("(?:[^/]+/)*")
					index += 2
				case pattern[index] == '*':
					expression.WriteString("[^/]*")
				case pattern[index] == '?':
					expression.WriteString("[^/]")
				default:
					expression.WriteString(regexp.QuoteMeta(pattern[index : index+1]))
				}
			}
			expression.WriteString("$")
			result[pattern] = regexp.MustCompile(expression.String())
		}
	}
	return result
}

type fileServer struct {
	ID, LanguageID string
	score          int
}

// The catalog uses basename globs and repository-relative path globs. Path
// rules take priority so workflows, Compose files, and templates use their
// dedicated server, with a generic YAML server available as a fallback.
func serversForPath(filename string) []fileServer {
	filename = strings.ToLower(filepath.ToSlash(filename))
	var result []fileServer
	for _, server := range serverCatalog {
		best := fileServer{ID: server.ID, score: -1}
		for _, association := range server.Files {
			pattern := strings.ToLower(association.Pattern)
			candidate := filename
			if !strings.Contains(pattern, "/") {
				candidate = filepath.Base(filename)
			}
			if !catalogPatterns[pattern].MatchString(candidate) {
				continue
			}
			score := len(strings.NewReplacer("*", "", "?", "").Replace(pattern))
			if strings.Contains(pattern, "/") {
				score += 1000
			}
			if score > best.score {
				best.LanguageID, best.score = association.LanguageID, score
			}
		}
		if best.score >= 0 {
			result = append(result, best)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].score > result[j].score })
	return result
}

func catalogStatus(language string) ServerStatus {
	server := catalogByID[language]
	if server == nil {
		return ServerStatus{Language: language, Args: []string{}}
	}
	return ServerStatus{Language: language, Name: server.Name, Label: server.Label, Category: server.Category,
		Description: server.Description, FileTypes: server.Files, InstallMethod: server.Install.Method,
		Requirements: server.Install.Requires, DefaultCommand: server.Program, DefaultArgs: server.Args, SetupURL: server.Homepage, Args: []string{}}
}

var catalogID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
