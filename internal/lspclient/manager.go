package lspclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var immutableSHA = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

type definitionGrant struct {
	target     DefinitionTarget
	workspace  *workspace
	path, text string
}
type Manager struct {
	options        Options
	ctx            context.Context
	cancel         context.CancelFunc
	gate           chan struct{}
	closed         bool
	configuration  map[string]ServerConfiguration
	configErr      error
	cacheDirectory string
	workspaces     map[string]*workspace
	order          []string
	aliases        map[string]string
	grants         map[string]definitionGrant
	grantOrder     []string
	grantBytes     int
	tools          *toolInstaller
}

func New(options Options) *Manager {
	if options.ConfigPath == "" {
		directory, err := os.UserConfigDir()
		if err == nil {
			options.ConfigPath = filepath.Join(directory, "mag-ik-een-review", "language-servers.json")
		}
	}
	if options.CacheDirectory == "" {
		directory, err := os.UserCacheDir()
		if err == nil {
			options.CacheDirectory = filepath.Join(directory, "mag-ik-een-review", "lsp")
		}
	}
	configuration, err := readConfigurations(options.ConfigPath)
	if configuration == nil {
		configuration = map[string]ServerConfiguration{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{options: options, ctx: ctx, cancel: cancel, gate: make(chan struct{}, 1), configuration: configuration, configErr: err, workspaces: map[string]*workspace{}, aliases: map[string]string{}, grants: map[string]definitionGrant{}}
	m.gate <- struct{}{}
	if options.ToolsDirectory == "" && options.ConfigPath != "" {
		options.ToolsDirectory = filepath.Join(filepath.Dir(options.ConfigPath), "language-tools")
	}
	m.tools = newToolInstaller(ctx, options.ToolsDirectory, options.ToolHTTPClient, func(ctx context.Context, commit func() error) error {
		release, err := m.lock(ctx)
		if err != nil {
			return err
		}
		defer release()
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = commit(); err != nil {
			return err
		}
		m.restartSessions()
		if m.options.OnToolsChanged != nil {
			m.options.OnToolsChanged()
		}
		return nil
	})
	return m
}
func (m *Manager) lock(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.ctx.Done():
		return nil, errors.New("Code intelligence has shut down")
	case <-m.gate:
		if m.closed {
			m.gate <- struct{}{}
			return nil, errors.New("Code intelligence has shut down")
		}
		return func() { m.gate <- struct{}{} }, nil
	}
}
func canonicalRoot(root string) (string, error) {
	if root == "" || strings.ContainsRune(root, 0) {
		return "", errors.New("Open a Git repository before using code intelligence")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if !existingDirectory(resolved) {
		return "", errors.New("Code intelligence needs a repository directory")
	}
	return resolved, nil
}
func (m *Manager) Servers(ctx context.Context, root string) ([]ServerStatus, error) {
	var err error
	if root != "" {
		root, err = canonicalRoot(root)
		if err != nil {
			return nil, err
		}
	}
	tools := m.tools.statuses(ctx)
	release, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if m.configErr != nil {
		return nil, m.configErr
	}
	result := []ServerStatus{}
	for _, language := range []string{"php", "typescript", "vue"} {
		status := m.detectServer(root, language)
		status.Managed = tools[language]
		if language == "vue" && status.Available {
			peer := m.vuePeer(root, status)
			if !peer.Available {
				status.Available = false
				status.Error = "Vue requires a TypeScript language server (version 4.4 or newer): " + peer.Error
			}
		}
		// Settings edit the saved configuration, not auto-detected SDK paths.
		status.Configuration = m.configuration[language]
		status.Configuration.Args = append([]string{}, status.Configuration.Args...)
		result = append(result, status)
	}
	return result, nil
}
func (m *Manager) Configure(ctx context.Context, language string, configuration ServerConfiguration) error {
	language, err := canonicalLanguage(language)
	if err != nil {
		return err
	}
	if err = validateConfiguration(configuration); err != nil {
		return err
	}
	if language == "php" && (configuration.TypeScriptSDKPath != "" || configuration.VuePluginPath != "") {
		return errors.New("TypeScript SDK and Vue plugin paths apply only to TypeScript / Vue servers")
	}
	release, err := m.lock(ctx)
	if err != nil {
		return err
	}
	defer release()
	next := map[string]ServerConfiguration{}
	for key, value := range m.configuration {
		next[key] = value
	}
	next[language] = configuration
	if m.options.ConfigPath == "" {
		return errors.New("OS configuration directory is unavailable")
	}
	if err = saveConfigurations(m.options.ConfigPath, next); err != nil {
		return fmt.Errorf("Save language server configuration: %w", err)
	}
	m.configuration = next
	m.configErr = nil
	m.restartSessions()
	return nil
}
func (m *Manager) restartSessions() {
	for _, w := range m.workspaces {
		for key, session := range w.sessions {
			_ = session.close()
			delete(w.sessions, key)
		}
	}
}
func (m *Manager) detectServer(root, language string) ServerStatus {
	managed, ready := m.tools.configuration(language)
	if !ready {
		managed = ServerConfiguration{}
	}
	return detectServer(root, language, m.configuration[language], managed)
}
func (m *Manager) vuePeer(root string, vue ServerStatus) ServerStatus {
	if vue.Source == "managed" {
		if managed, ready := m.tools.configuration("vue-typescript"); ready {
			return detectServer(root, "typescript", ServerConfiguration{}, managed)
		}
	}
	return m.detectServer(root, "typescript")
}
func (m *Manager) CheckServerUpdates(ctx context.Context, root string, force bool) ([]ServerStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	stop := context.AfterFunc(m.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err := m.tools.checkUpdates(ctx, force); err != nil {
		return nil, err
	}
	return m.Servers(ctx, root)
}
func (m *Manager) InstallServer(ctx context.Context, language string) error {
	return m.tools.start(ctx, language)
}
func (m *Manager) CancelServerInstall(language string) error {
	return m.tools.cancel(language)
}
func (m *Manager) requestContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	stop := context.AfterFunc(m.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}
func (m *Manager) prepare(ctx context.Context, request CodePositionRequest) (*workspace, *session, string, string, string, func(), error) {
	root, err := canonicalRoot(request.Root)
	if err != nil {
		return nil, nil, "", "", "", nil, err
	}
	request.Root = root
	mutable := request.Revision == "working" || request.Revision == "index"
	if (!mutable && !immutableSHA.MatchString(request.Revision)) || (request.ComparisonHead != "" && (mutable || !immutableSHA.MatchString(request.ComparisonHead))) {
		return nil, nil, "", "", "", nil, errors.New("Choose the complete immutable commit SHA shown by the diff")
	}
	request.Revision = strings.ToLower(request.Revision)
	request.ComparisonHead = strings.ToLower(request.ComparisonHead)
	release, err := m.lock(ctx)
	if err != nil {
		return nil, nil, "", "", "", nil, err
	}
	defer release()
	if m.configErr != nil {
		return nil, nil, "", "", "", nil, m.configErr
	}
	var w *workspace
	var path, text, language string
	if strings.HasPrefix(request.Path, "mier-lsp://definition/") {
		grant, ok := m.grants[request.Path]
		if !ok || grant.workspace.root != root || grant.workspace.revision != request.Revision {
			return nil, nil, "", "", "", nil, errors.New("Definition permission expired or belongs to another revision. Open the definition again")
		}
		w = grant.workspace
		path = grant.path
		text = grant.text
		language = grant.target.Language
	} else {
		if mutable && request.LineText == nil {
			return nil, nil, "", "", "", nil, errors.New("Local symbol inspection needs the displayed line text to verify its source position")
		}
		relative, err := repositoryPath(request.Path)
		if err != nil {
			return nil, nil, "", "", "", nil, err
		}
		language = languageForPath(relative)
		if language == "" {
			return nil, nil, "", "", "", nil, errors.New("Code intelligence currently supports PHP, TypeScript, JavaScript, and Vue files")
		}
		alias := root + "\x00" + request.Revision + "\x00" + request.ComparisonHead
		key := root + "\x00" + request.Revision
		if request.ComparisonHead != "" {
			key = m.aliases[alias]
		}
		w = m.workspaces[key]
		if w == nil || mutable {
			if m.options.Archive == nil {
				return nil, nil, "", "", "", nil, errors.New("Immutable Git snapshots are unavailable")
			}
			archive, err := m.options.Archive(ctx, request)
			if err != nil {
				return nil, nil, "", "", "", nil, err
			}
			if !immutableSHA.MatchString(archive.Revision) {
				return nil, nil, "", "", "", nil, errors.New("Git returned an invalid snapshot revision")
			}
			if !mutable && request.ComparisonHead == "" && !strings.EqualFold(archive.Revision, request.Revision) {
				return nil, nil, "", "", "", nil, errors.New("Git snapshot does not match the selected commit")
			}
			key = root + "\x00" + strings.ToLower(archive.Revision)
			w = m.workspaces[key]
			if w == nil {
				if err = m.evictWorkspace(); err != nil {
					return nil, nil, "", "", "", nil, err
				}
				if m.cacheDirectory == "" {
					if m.options.CacheDirectory == "" {
						return nil, nil, "", "", "", nil, errors.New("OS cache directory is unavailable")
					}
					if err = os.MkdirAll(m.options.CacheDirectory, 0700); err != nil {
						return nil, nil, "", "", "", nil, err
					}
					m.cacheDirectory, err = os.MkdirTemp(m.options.CacheDirectory, "session-")
					if err != nil {
						return nil, nil, "", "", "", nil, err
					}
				}
				w, err = extractWorkspace(ctx, root, m.cacheDirectory, archive)
				if err != nil {
					return nil, nil, "", "", "", nil, err
				}
				m.workspaces[key] = w
				m.order = append(m.order, key)
			}
			m.aliases[alias] = key
		}
		path = filepath.Join(w.directory, relative)
		text, _, _, err = w.read(path)
		if err != nil {
			return nil, nil, "", "", "", nil, fmt.Errorf("Read selected revision file: %w", err)
		}
	}
	if err = validatePosition(text, request); err != nil {
		return nil, nil, "", "", "", nil, err
	}
	s, err := m.ensureSession(ctx, w, serverLanguage(language))
	if err != nil {
		return nil, nil, "", "", "", nil, err
	}
	w.leases++
	done := func() { <-m.gate; w.leases--; m.gate <- struct{}{} }
	return w, s, path, text, language, done, nil
}
func (m *Manager) ensureSession(ctx context.Context, w *workspace, language string) (*session, error) {
	if existing := w.sessions[language]; existing != nil && existing.rpc.Alive() && (existing.peer == nil || existing.peer.rpc.Alive()) {
		return existing, nil
	}
	if existing := w.sessions[language]; existing != nil {
		_ = existing.close()
		delete(w.sessions, language)
	}
	status := m.detectServer(w.root, language)
	if !status.Available {
		return nil, fmt.Errorf("%s. Setup: %s", status.Error, status.SetupURL)
	}
	var peer *session
	if language == "vue" {
		peerStatus := m.vuePeer(w.root, status)
		if !peerStatus.Available {
			return nil, fmt.Errorf("Vue requires TypeScript language-server 4.4 or newer: %s", peerStatus.Error)
		}
		peerStatus.Configuration.VuePluginPath = status.Configuration.VuePluginPath
		peerStatus.Configuration.TypeScriptSDKPath = status.Configuration.TypeScriptSDKPath
		peer = w.sessions["vue-typescript"]
		if peer == nil || !peer.rpc.Alive() {
			var err error
			peer, err = startSession(m.ctx, ctx, w, peerStatus, nil)
			if err != nil {
				return nil, err
			}
			if !peer.supportsVueBroker() {
				peer.close()
				return nil, errors.New("Vue requires typescript-language-server 4.4 or newer with typescript.tsserverRequest support")
			}
			w.sessions["vue-typescript"] = peer
		}
	}
	if status.Configuration.TypeScriptSDKPath != "" {
		if err := w.addScope(status.Configuration.TypeScriptSDKPath, "[TypeScript SDK]", true); err != nil {
			return nil, err
		}
	}
	if status.Configuration.VuePluginPath != "" {
		if err := w.addScope(status.Configuration.VuePluginPath, "[Vue plugin]", true); err != nil {
			return nil, err
		}
	}
	s, err := startSession(m.ctx, ctx, w, status, peer)
	if err != nil {
		return nil, err
	}
	w.sessions[language] = s
	return s, nil
}
func (m *Manager) evictWorkspace() error {
	if len(m.workspaces) < 4 {
		return nil
	}
	for index, key := range m.order {
		w := m.workspaces[key]
		if w.leases != 0 {
			continue
		}
		w.close()
		delete(m.workspaces, key)
		m.order = append(m.order[:index], m.order[index+1:]...)
		for alias, target := range m.aliases {
			if target == key {
				delete(m.aliases, alias)
			}
		}
		for uri, grant := range m.grants {
			if grant.workspace == w {
				m.grantBytes -= len(grant.text)
				delete(m.grants, uri)
			}
		}
		return nil
	}
	return errors.New("All four cached code-analysis revisions are busy. Try again after the current request finishes")
}
func (m *Manager) Hover(ctx context.Context, request CodePositionRequest) (Hover, error) {
	ctx, cancel := m.requestContext(ctx)
	defer cancel()
	w, s, path, text, language, done, err := m.prepare(ctx, request)
	if err != nil {
		return Hover{}, err
	}
	defer done()
	unlock, err := s.lock(ctx)
	if err != nil {
		return Hover{}, err
	}
	defer unlock()
	if s.peer != nil {
		if err = s.peer.open(ctx, path, language, text); err != nil {
			return Hover{}, err
		}
	}
	if err = s.open(ctx, path, language, text); err != nil {
		return Hover{}, err
	}
	var result struct {
		Contents json.RawMessage `json:"contents"`
		Range    *Range          `json:"range"`
	}
	err = s.rpc.Request(ctx, "textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": fileURI(path)}, "position": Position{request.Line, request.Character}}, &result)
	if s.peer != nil && (err == nil && hoverContents(result.Contents) == "" || isUnsupportedMethod(err)) {
		err = s.peer.rpc.Request(ctx, "textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": fileURI(path)}, "position": Position{request.Line, request.Character}}, &result)
	}
	if err != nil {
		return Hover{}, err
	}
	contents := hoverContents(result.Contents)
	if len(contents) > 64*1024 {
		contents = strings.ToValidUTF8(contents[:64*1024], "") + "\n\n…"
	}
	if result.Range != nil {
		if !validRange(*result.Range) || validatePosition(text, CodePositionRequest{Line: result.Range.Start.Line, Character: result.Range.Start.Character}) != nil || validatePosition(text, CodePositionRequest{Line: result.Range.End.Line, Character: result.Range.End.Character}) != nil {
			result.Range = nil
		}
	}
	return Hover{Contents: contents, Kind: hoverKind(result.Contents), Range: result.Range, Revision: w.revision, Language: language}, nil
}
func (m *Manager) Definition(ctx context.Context, request CodePositionRequest) ([]DefinitionTarget, error) {
	ctx, cancel := m.requestContext(ctx)
	defer cancel()
	w, s, path, text, language, done, err := m.prepare(ctx, request)
	if err != nil {
		return nil, err
	}
	defer done()
	unlock, err := s.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if s.peer != nil {
		if err = s.peer.open(ctx, path, language, text); err != nil {
			return nil, err
		}
	}
	if err = s.open(ctx, path, language, text); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	err = s.rpc.Request(ctx, "textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": fileURI(path)}, "position": Position{request.Line, request.Character}}, &raw)
	if isUnsupportedMethod(err) && s.peer != nil {
		err = s.peer.rpc.Request(ctx, "textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": fileURI(path)}, "position": Position{request.Line, request.Character}}, &raw)
	}
	if err != nil {
		return nil, err
	}
	locations, err := parseDefinitions(raw)
	if err != nil {
		return nil, err
	}
	if len(locations) == 0 && s.peer != nil {
		if err = s.peer.rpc.Request(ctx, "textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": fileURI(path)}, "position": Position{request.Line, request.Character}}, &raw); err != nil {
			return nil, err
		}
		locations, err = parseDefinitions(raw)
		if err != nil {
			return nil, err
		}
	}
	targets := []DefinitionTarget{}
	release, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	for _, location := range locations {
		targetPath, err := pathFromURI(location.URI)
		if err != nil {
			continue
		}
		content, label, external, err := w.read(targetPath)
		if err != nil {
			continue
		}
		if languageForPath(targetPath) == "" || !validRange(location.Range) {
			continue
		}
		if err = validatePosition(content, CodePositionRequest{Line: location.Range.Start.Line, Character: location.Range.Start.Character}); err != nil {
			continue
		}
		if validatePosition(content, CodePositionRequest{Line: location.Range.End.Line, Character: location.Range.End.Character}) != nil {
			continue
		}
		token := make([]byte, 16)
		if _, err = rand.Read(token); err != nil {
			return nil, err
		}
		id := hex.EncodeToString(token)
		uri := "mier-lsp://definition/" + id
		target := DefinitionTarget{ID: id, URI: uri, Path: label, Range: location.Range, Revision: w.revision, Language: languageForPath(targetPath), External: external}
		for len(m.grantOrder) > 0 && (len(m.grants) >= 64 || m.grantBytes+len(content) > 32*1024*1024) {
			old := m.grantOrder[0]
			m.grantOrder = m.grantOrder[1:]
			if grant, ok := m.grants[old]; ok {
				m.grantBytes -= len(grant.text)
				delete(m.grants, old)
			}
		}
		m.grants[uri] = definitionGrant{target: target, workspace: w, path: targetPath, text: content}
		m.grantOrder = append(m.grantOrder, uri)
		m.grantBytes += len(content)
		targets = append(targets, target)
	}
	if len(locations) > 0 && len(targets) == 0 {
		return nil, errors.New("Language server definitions are outside the selected commit or known dependency directories")
	}
	return targets, nil
}
func (m *Manager) ReadDefinition(ctx context.Context, request ReadDefinitionRequest) (DefinitionDocument, error) {
	root, err := canonicalRoot(request.Root)
	if err != nil {
		return DefinitionDocument{}, err
	}
	release, err := m.lock(ctx)
	if err != nil {
		return DefinitionDocument{}, err
	}
	defer release()
	grant, ok := m.grants[request.Target]
	if !ok || grant.workspace.root != root || grant.workspace.revision != strings.ToLower(request.Revision) {
		return DefinitionDocument{}, errors.New("Definition permission expired or belongs to another repository revision")
	}
	target := grant.target
	return DefinitionDocument{URI: target.URI, Text: grant.text, Path: target.Path, Language: target.Language, Revision: target.Revision, Range: target.Range, External: target.External}, nil
}
func (m *Manager) Close() error {
	m.cancel()
	m.tools.close()
	<-m.gate
	defer func() { m.gate <- struct{}{} }()
	if m.closed {
		return nil
	}
	m.closed = true
	for _, w := range m.workspaces {
		w.close()
	}
	m.workspaces = map[string]*workspace{}
	m.grants = map[string]definitionGrant{}
	if m.cacheDirectory != "" {
		return os.RemoveAll(m.cacheDirectory)
	}
	return nil
}

type definitionLocation struct {
	URI   string
	Range Range
}

func isUnsupportedMethod(err error) bool {
	var rpcError *RPCError
	return errors.As(err, &rpcError) && rpcError.Code == -32601
}

func validRange(value Range) bool {
	return value.Start.Line >= 0 && value.Start.Character >= 0 && value.End.Line >= value.Start.Line && value.End.Character >= 0 && (value.End.Line > value.Start.Line || value.End.Character >= value.Start.Character)
}
func parseDefinitions(raw json.RawMessage) ([]definitionLocation, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	} else {
		items = []json.RawMessage{raw}
	}
	if len(items) > 32 {
		return nil, errors.New("Language server returned more than 32 definition targets")
	}
	locations := []definitionLocation{}
	for _, item := range items {
		var location struct {
			URI                  string `json:"uri"`
			Range                Range  `json:"range"`
			TargetURI            string `json:"targetUri"`
			TargetRange          Range  `json:"targetRange"`
			TargetSelectionRange *Range `json:"targetSelectionRange"`
		}
		if err := json.Unmarshal(item, &location); err != nil {
			return nil, err
		}
		if location.TargetURI != "" {
			rangeValue := location.TargetRange
			if location.TargetSelectionRange != nil {
				rangeValue = *location.TargetSelectionRange
			}
			locations = append(locations, definitionLocation{location.TargetURI, rangeValue})
		} else if location.URI != "" {
			locations = append(locations, definitionLocation{location.URI, location.Range})
		}
	}
	return locations, nil
}
