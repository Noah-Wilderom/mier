package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zalando/go-keyring"
	"io"
	"mag-ik-een-review/internal/gitclient"
	"mag-ik-een-review/internal/githubclient"
	"mag-ik-een-review/internal/reviewproviders"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const reviewCredentialService = "mag-ik-een-review.review-providers"

type ReviewConnection struct {
	Kind         string `json:"kind"`
	Host         string `json:"host"`
	Username     string `json:"username"`
	Organization string `json:"organization"`
	ReadOnly     bool   `json:"readOnly"`
	Login        string `json:"login"`
	Name         string `json:"name"`
	AvatarURL    string `json:"avatarUrl"`
	Disabled     bool   `json:"disabled"`
}
type ReviewConnectRequest struct {
	Kind         string `json:"kind"`
	Host         string `json:"host"`
	Username     string `json:"username"`
	Organization string `json:"organization"`
	ReadOnly     bool   `json:"readOnly"`
	Token        string `json:"token"`
}
type ReviewProviderService struct {
	mu          sync.Mutex
	store       secretStore
	configPath  string
	configErr   error
	connections map[string]ReviewConnection
	clients     map[string]reviewproviders.API
	publisher   *reviewproviders.Publisher
	factory     func(ReviewConnection, string, reviewproviders.Options) (reviewproviders.API, error)
}

func NewReviewProviderService(credentials *CredentialService) *ReviewProviderService {
	directory, err := os.UserConfigDir()
	s := &ReviewProviderService{store: credentials.store, configPath: filepath.Join(directory, "mag-ik-een-review", "review-providers.json"), configErr: err, connections: map[string]ReviewConnection{}, clients: map[string]reviewproviders.API{}}
	s.publisher = reviewproviders.NewPublisher(&reviewProgressFiles{directory: filepath.Join(directory, "mag-ik-een-review", "review-publications")})
	diffClient, diffErr := gitclient.New(gitclient.Options{})
	s.factory = func(connection ReviewConnection, token string, options reviewproviders.Options) (reviewproviders.API, error) {
		options.Organization = connection.Organization
		options.ReadOnly = connection.ReadOnly
		switch connection.Kind {
		case "gitlab":
			return reviewproviders.NewGitLab(connection.Host, token, options)
		case "bitbucket":
			return reviewproviders.NewBitbucket(connection.Host, connection.Username, token, options)
		case "azure":
			if diffErr != nil {
				return nil, fmt.Errorf("Git is required to compare Azure source files: %w", diffErr)
			}
			options.Diff = diffClient.DiffText
			return reviewproviders.NewAzure(connection.Host, token, options)
		default:
			return nil, errors.New("This review provider is not available")
		}
	}
	if err == nil {
		data, readErr := readReviewSettingsFile(s.configPath)
		if readErr == nil {
			var items []ReviewConnection
			if json.Unmarshal(data, &items) == nil {
				for _, connection := range items {
					host, err := normalizeReviewHost(connection.Host)
					if err == nil && validReviewKind(connection.Kind) && host != "" {
						connection.Host = host
						s.connections[host] = connection
					}
				}
			} else {
				s.configErr = errors.New("Saved provider settings are unreadable; keep a backup before repairing the file at " + s.configPath)
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			s.configErr = fmt.Errorf("Read saved provider settings: %w", readErr)
		}
	}
	return s
}

func readReviewSettingsFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 2*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 2*1024*1024 {
		return nil, errors.New("Saved provider settings exceed 2 MiB")
	}
	return data, nil
}
func validReviewKind(kind string) bool {
	return kind == "gitlab" || kind == "bitbucket" || kind == "azure"
}
func normalizeReviewHost(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("Enter a provider hostname")
	}
	host, err := githubclient.NormalizeHost(value)
	if err != nil {
		return "", errors.New("Enter an HTTPS provider hostname without credentials or a repository path")
	}
	if host == "api.bitbucket.org" {
		host = "bitbucket.org"
	}
	if host == "ssh.dev.azure.com" || strings.HasSuffix(host, ".visualstudio.com") {
		host = "dev.azure.com"
	}
	return host, nil
}
func (s *ReviewProviderService) Kind(input string) string {
	host, err := normalizeReviewHost(input)
	if err != nil {
		return ""
	}
	s.mu.Lock()
	connection, ok := s.connections[host]
	s.mu.Unlock()
	if ok {
		return connection.Kind
	}
	return map[string]string{"gitlab.com": "gitlab", "bitbucket.org": "bitbucket", "dev.azure.com": "azure"}[host]
}
func (s *ReviewProviderService) Connections() ([]ReviewConnection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configErr != nil {
		return nil, s.configErr
	}
	items := []ReviewConnection{}
	for _, connection := range s.connections {
		items = append(items, connection)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Host < items[j].Host })
	return items, nil
}
func (s *ReviewProviderService) Supported() []string { return []string{"gitlab", "bitbucket", "azure"} }
func (s *ReviewProviderService) persistLocked() error {
	if s.configErr != nil {
		return s.configErr
	}
	items := []ReviewConnection{}
	for _, connection := range s.connections {
		items = append(items, connection)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Host < items[j].Host })
	return writePrivateJSON(s.configPath, items)
}
func writePrivateJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".mier-settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = file.Chmod(0600); err != nil {
		return err
	}
	if err = json.NewEncoder(file).Encode(value); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func reviewSecretKey(connection ReviewConnection) string {
	return connection.Kind + ":" + connection.Host
}
func reviewClientKey(connection ReviewConnection, token string) string {
	data, _ := json.Marshal(connection)
	sum := sha256.Sum256(append(data, []byte(token)...))
	return connection.Host + ":" + hex.EncodeToString(sum[:])
}
func (s *ReviewProviderService) Connect(ctx context.Context, request ReviewConnectRequest) (AccountStatus, error) {
	if s.configErr != nil {
		return AccountStatus{}, s.configErr
	}
	host, err := normalizeReviewHost(request.Host)
	if err != nil {
		return AccountStatus{}, err
	}
	if !validReviewKind(request.Kind) {
		return AccountStatus{}, errors.New("Choose GitLab, Bitbucket Cloud, or Azure DevOps")
	}
	if host == "github.com" {
		return AccountStatus{}, errors.New("Use the GitHub account controls for github.com")
	}
	if existingKind := s.Kind(host); existingKind != "" && existingKind != request.Kind {
		return AccountStatus{}, errors.New("This hostname is already associated with a different review provider")
	}
	token := strings.TrimSpace(request.Token)
	if token == "" || len(token) > 16*1024 || strings.ContainsAny(token, "\x00\r\n") {
		return AccountStatus{}, errors.New("Enter a valid provider token")
	}
	if len(request.Username) > 512 || strings.ContainsAny(request.Username, "\x00\r\n") || len(request.Organization) > 256 || strings.ContainsAny(request.Organization, "\x00\r\n/\\") {
		return AccountStatus{}, errors.New("Enter a valid provider email and organization")
	}
	connection := ReviewConnection{Kind: request.Kind, Host: host, Username: strings.TrimSpace(request.Username), Organization: strings.TrimSpace(request.Organization), ReadOnly: request.ReadOnly}
	client, err := s.factory(connection, token, reviewproviders.Options{Publisher: s.publisher})
	if err != nil {
		return AccountStatus{}, err
	}
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return AccountStatus{}, err
	}
	if user.Login == "" || len(user.Login) > 512 {
		return AccountStatus{}, errors.New("The provider did not return a valid authenticated account")
	}
	connection.Login = user.Login
	connection.Name = user.Name
	if len(connection.Name) > 512 {
		connection.Name = connection.Name[:512]
	}
	connection.AvatarURL = user.AvatarURL
	if len(connection.AvatarURL) > 4096 {
		connection.AvatarURL = ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.connections[host]
	previousToken, previousTokenErr := s.store.Get(reviewCredentialService, reviewSecretKey(connection))
	if err = s.store.Set(reviewCredentialService, reviewSecretKey(connection), token); err != nil {
		return AccountStatus{}, errors.New("Provider credentials could not be saved in the OS keychain. Unlock it and retry")
	}
	s.connections[host] = connection
	if err = s.persistLocked(); err != nil {
		if existed {
			s.connections[host] = previous
		} else {
			delete(s.connections, host)
		}
		if previousTokenErr == nil {
			_ = s.store.Set(reviewCredentialService, reviewSecretKey(connection), previousToken)
		} else {
			_ = s.store.Delete(reviewCredentialService, reviewSecretKey(connection))
		}
		return AccountStatus{}, fmt.Errorf("Provider settings could not be saved: %w", err)
	}
	for key := range s.clients {
		if strings.HasPrefix(key, host+":") {
			delete(s.clients, key)
		}
	}
	s.clients[reviewClientKey(connection, token)] = client
	return AccountStatus{Host: host, Authenticated: true, Login: user.Login, Name: user.Name, AvatarURL: user.AvatarURL, Source: "saved", Provider: connection.Kind, ReadOnly: connection.ReadOnly}, nil
}
func (s *ReviewProviderService) credentials(input string) (ReviewConnection, string, error) {
	host, err := normalizeReviewHost(input)
	if err != nil {
		return ReviewConnection{}, "", err
	}
	s.mu.Lock()
	connection, ok := s.connections[host]
	s.mu.Unlock()
	if !ok || connection.Disabled {
		return ReviewConnection{}, "", errors.New("Connect this provider in Settings → Accounts")
	}
	token, err := s.store.Get(reviewCredentialService, reviewSecretKey(connection))
	if err != nil || strings.TrimSpace(token) == "" {
		return connection, "", errors.New("The provider token is unavailable. Unlock your OS keychain or reconnect the account")
	}
	return connection, token, nil
}
func (s *ReviewProviderService) client(input string) (reviewproviders.API, error) {
	connection, token, err := s.credentials(input)
	if err != nil {
		return nil, err
	}
	key := reviewClientKey(connection, token)
	s.mu.Lock()
	defer s.mu.Unlock()
	if client, ok := s.clients[key]; ok {
		return client, nil
	}
	client, err := s.factory(connection, token, reviewproviders.Options{Publisher: s.publisher})
	if err != nil {
		return nil, err
	}
	for old := range s.clients {
		if strings.HasPrefix(old, connection.Host+":") {
			delete(s.clients, old)
		}
	}
	s.clients[key] = client
	return client, nil
}
func (s *ReviewProviderService) Status(ctx context.Context, input string) (AccountStatus, error) {
	host, err := normalizeReviewHost(input)
	if err != nil {
		return AccountStatus{}, err
	}
	status := AccountStatus{Host: host, Provider: s.Kind(host), Source: "saved"}
	connection, _, err := s.credentials(host)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}
	status.ReadOnly = connection.ReadOnly
	client, err := s.client(host)
	if err != nil {
		return status, err
	}
	user, err := client.CurrentUser(ctx)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}
	status.Authenticated = true
	status.Login = user.Login
	status.Name = user.Name
	status.AvatarURL = user.AvatarURL
	return status, nil
}
func (s *ReviewProviderService) Disconnect(ctx context.Context, input string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	host, err := normalizeReviewHost(input)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	connection, ok := s.connections[host]
	if !ok {
		return nil
	}
	if err = s.store.Delete(reviewCredentialService, reviewSecretKey(connection)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return errors.New("The provider token could not be removed from the OS keychain")
	}
	connection.Login = ""
	connection.Name = ""
	connection.AvatarURL = ""
	connection.Username = ""
	connection.Organization = ""
	connection.Disabled = true
	s.connections[host] = connection
	for key := range s.clients {
		if strings.HasPrefix(key, host+":") {
			delete(s.clients, key)
		}
	}
	return s.persistLocked()
}
func (s *ReviewProviderService) gitCredential(ctx context.Context, input string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	connection, token, err := s.credentials(input)
	if err != nil {
		return "", "", err
	}
	username := "oauth2"
	switch connection.Kind {
	case "bitbucket":
		username = "x-bitbucket-api-token-auth"
	case "azure":
		username = "mier"
	}
	return username, token, nil
}

type reviewProgressFiles struct {
	mu        sync.Mutex
	directory string
}

func validProgressKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil && strings.ToLower(key) == key
}
func (s *reviewProgressFiles) Load(key string) (reviewproviders.PublicationProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result reviewproviders.PublicationProgress
	if !validProgressKey(key) {
		return result, errors.New("Invalid review progress key")
	}
	data, err := readReviewSettingsFile(filepath.Join(s.directory, key+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if len(data) > 2*1024*1024 {
		return result, errors.New("Saved review progress exceeds 2 MiB")
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return result, errors.New("Saved review progress is unreadable")
	}
	return result, nil
}
func (s *reviewProgressFiles) Save(key string, value reviewproviders.PublicationProgress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validProgressKey(key) || len(value.Comments) > 10000 {
		return errors.New("The saved review progress exceeds its limit")
	}
	return writePrivateJSON(filepath.Join(s.directory, key+".json"), value)
}

func (s *ReviewProviderService) reviewSource(ctx context.Context, host, owner, repo string, number int) (gitclient.PullRequestFetchSource, error) {
	client, err := s.client(host)
	if err != nil {
		return gitclient.PullRequestFetchSource{}, err
	}
	reader, ok := client.(reviewproviders.SourceReader)
	if !ok {
		return gitclient.PullRequestFetchSource{}, errors.New("This provider did not expose an immutable review source")
	}
	source, err := reader.PullRequestSource(ctx, owner, repo, number)
	if err != nil {
		return gitclient.PullRequestFetchSource{}, err
	}
	clean := func(value string) (string, error) {
		if value == "" {
			return "", nil
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", errors.New("The provider returned an unsupported source URL")
		}
		canonical, err := normalizeReviewHost(parsed.Host)
		expected, _ := normalizeReviewHost(host)
		if err != nil || canonical != expected {
			return "", errors.New("The review source is outside its provider's configured HTTPS host")
		}
		if parsed.User != nil {
			if _, password := parsed.User.Password(); password {
				return "", errors.New("Provider source URLs cannot contain a password")
			}
			parsed.User = nil
		}
		return parsed.String(), nil
	}
	head, err := clean(source.HeadURL)
	if err != nil {
		return gitclient.PullRequestFetchSource{}, err
	}
	base, err := clean(source.BaseURL)
	if err != nil {
		return gitclient.PullRequestFetchSource{}, err
	}
	return gitclient.PullRequestFetchSource{HeadURL: head, HeadRef: source.HeadRef, HeadSHA: source.HeadSHA, BaseURL: base, BaseRef: source.BaseRef}, nil
}
