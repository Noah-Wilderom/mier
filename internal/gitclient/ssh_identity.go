package gitclient

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

type sshIdentity struct {
	host  string
	until time.Time
}

func sshRemote(raw string) bool {
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		return err == nil && strings.EqualFold(parsed.Scheme, "ssh")
	}
	return scpRemote.MatchString(raw)
}
func (c *Client) sshHost(ctx context.Context, alias string) (string, error) {
	if alias == "" || strings.ContainsAny(alias, ":[]\x00\r\n") || strings.HasPrefix(alias, "-") {
		return alias, nil
	}
	c.sshMu.Lock()
	cached := c.sshHosts[alias]
	c.sshMu.Unlock()
	if time.Now().Before(cached.until) {
		return cached.host, nil
	}
	resolver := c.options.SSHHostResolver
	if resolver == nil {
		resolver = resolveSSHHost
	}
	host, err := resolver(ctx, alias)
	if err != nil {
		return "", err
	}
	if host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, "/\\:@ \r\n\x00") || len(host) > 253 {
		return "", errors.New("SSH returned an invalid hostname")
	}
	host = strings.ToLower(host)
	c.sshMu.Lock()
	c.sshHosts[alias] = sshIdentity{host: host, until: time.Now().Add(time.Minute)}
	c.sshMu.Unlock()
	return host, nil
}
func resolveSSHHost(ctx context.Context, alias string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "ssh", "-G", "--", alias)
	output := &cappedBuffer{limit: 64 * 1024}
	command.Stdout = output
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	cleanup := configureCommand(command)
	defer cleanup()
	if err := command.Run(); err != nil {
		return "", errors.New("SSH host configuration is unavailable")
	}
	if output.overflow {
		return "", errors.New("SSH host configuration exceeds its limit")
	}
	for _, line := range strings.Split(output.String(), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && strings.EqualFold(key, "hostname") {
			return strings.TrimSpace(value), nil
		}
	}
	return alias, nil
}
