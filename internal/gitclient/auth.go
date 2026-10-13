package gitclient

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Authentication is scoped to one process and HTTPS origin. Secrets are never
// written to a Git configuration file or passed as command-line arguments.
func (c *Client) authenticatedRun(ctx context.Context, root string, timeout time.Duration, remoteURL string, args ...string) ([]byte, error) {
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return nil, errors.New("Invalid Git remote URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	secrets := []string{}
	if parsed.User != nil {
		secrets = append(secrets, parsed.User.Username())
		if password, exists := parsed.User.Password(); exists {
			secrets = append(secrets, password)
		}
	}
	for _, values := range parsed.Query() {
		secrets = append(secrets, values...)
	}
	if parsed.Scheme != "https" {
		return c.execute(ctx, root, timeout, nil, secrets, args...)
	}

	credential := ""
	if parsed.User != nil {
		username := parsed.User.Username()
		password, exists := parsed.User.Password()
		if !exists || password == "" {
			credential = "x-access-token:" + username
		} else {
			credential = username + ":" + password
		}
	} else if c.options.CredentialProvider != nil {
		token, providerErr := c.options.CredentialProvider(ctx, strings.ToLower(parsed.Host))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Public repositories and an existing Git credential helper continue to
		// work when there is no account available in the application.
		if providerErr == nil && strings.TrimSpace(token) != "" {
			token = strings.TrimSpace(token)
			if strings.ContainsAny(token, "\r\n\x00") || len(token) > 16*1024 {
				return nil, errors.New("The stored Git token is invalid; reconnect the account")
			}
			credential = "x-access-token:" + token
			secrets = append(secrets, token)
		}
	}
	if credential == "" {
		return c.execute(ctx, root, timeout, nil, secrets, args...)
	}
	if strings.ContainsAny(credential, "\r\n\x00") || len(credential) > 32*1024 {
		return nil, errors.New("Invalid credentials in Git repository URL")
	}
	count := 0
	if value := os.Getenv("GIT_CONFIG_COUNT"); value != "" {
		count, err = strconv.Atoi(value)
		if err != nil || count < 0 || count > 1024 {
			return nil, errors.New("The Git environment has an invalid configuration count")
		}
	}
	origin := "https://" + strings.ToLower(parsed.Host) + "/"
	cleanURL := *parsed
	cleanURL.User = nil
	encoded := base64.StdEncoding.EncodeToString([]byte(credential))
	entries := [][2]string{
		{"http." + origin + ".extraheader", ""},
		{"http." + origin + ".extraheader", "Authorization: Basic " + encoded},
		{"http.followRedirects", "false"},
		{"http." + origin + ".followRedirects", "false"},
		{"http." + cleanURL.String() + ".followRedirects", "false"},
	}
	extraEnv := []string{"GIT_CONFIG_COUNT=" + strconv.Itoa(count+len(entries))}
	for index, entry := range entries {
		extraEnv = append(extraEnv, "GIT_CONFIG_KEY_"+strconv.Itoa(count+index)+"="+entry[0], "GIT_CONFIG_VALUE_"+strconv.Itoa(count+index)+"="+entry[1])
	}
	secrets = append(secrets, encoded)
	return c.execute(ctx, root, timeout, extraEnv, secrets, args...)
}
