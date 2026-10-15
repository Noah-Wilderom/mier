package gitclient

import (
	"context"
	"errors"
	"strings"
)

type Tag struct {
	Name      string `json:"name"`
	Object    string `json:"object"`
	Commit    string `json:"commit"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
	Annotated bool   `json:"annotated"`
}
type TagRequest struct {
	Action         string `json:"action"`
	Name           string `json:"name"`
	NewName        string `json:"newName"`
	SHA            string `json:"sha"`
	Message        string `json:"message"`
	ExpectedObject string `json:"expectedObject"`
	Remote         string `json:"remote"`
}
type RemoteRequest struct {
	Action           string `json:"action"`
	Name             string `json:"name"`
	NewName          string `json:"newName"`
	URL              string `json:"url"`
	ExpectedFetchURL string `json:"expectedFetchUrl"`
	ExpectedPushURL  string `json:"expectedPushUrl"`
}
type SigningConfig struct {
	Enabled bool   `json:"enabled"`
	Format  string `json:"format"`
	Key     string `json:"key"`
}
type CommitSignature struct {
	Status string `json:"status"`
	Signer string `json:"signer"`
	Key    string `json:"key"`
}

func (c *Client) Tags(ctx context.Context, path string) ([]Tag, error) {
	items := []Tag{}
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		out, err := c.run(ctx, root, c.options.LocalTimeout, "for-each-ref", "--sort=-creatordate", "--format=%(refname:strip=2)%00%(objectname)%00%(objecttype)%00%(*objectname)%00%(subject)%00%(creatordate:iso-strict)", "refs/tags")
		if err != nil {
			return err
		}
		for _, row := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
			if row == "" {
				continue
			}
			f := strings.Split(row, "\x00")
			if len(f) != 6 {
				return errors.New("Invalid tag record")
			}
			commit := f[3]
			if f[2] == "commit" {
				commit = f[1]
			}
			items = append(items, Tag{f[0], f[1], commit, f[4], f[5], f[2] == "tag"})
		}
		return nil
	})
	return items, err
}
func (c *Client) validateTag(ctx context.Context, root, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsRune(name, 0) {
		return errors.New("Choose a valid tag name")
	}
	_, err := c.run(ctx, root, c.options.LocalTimeout, "check-ref-format", "refs/tags/"+name)
	return err
}
func (c *Client) MutateTag(ctx context.Context, path string, request TagRequest) error {
	return c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		if err := c.validateTag(ctx, root, request.Name); err != nil {
			return err
		}
		ref := "refs/tags/" + request.Name
		if request.Action == "create" {
			if !objectID.MatchString(request.SHA) {
				return errors.New("Choose a full commit ID")
			}
			if _, err := c.resolveRevision(ctx, root, request.SHA); err != nil {
				return err
			}
			if len(request.Message) > 100*1024 || strings.ContainsRune(request.Message, 0) {
				return errors.New("Invalid tag message")
			}
			args := []string{"tag"}
			if strings.TrimSpace(request.Message) != "" {
				args = append(args, "--annotate", "--message", request.Message)
			}
			_, err := c.run(ctx, root, c.options.LocalTimeout, append(args, "--", request.Name, request.SHA)...)
			return err
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "show-ref", "--verify", "--hash", ref)
		if err != nil {
			return err
		}
		if !objectID.MatchString(request.ExpectedObject) || strings.TrimSpace(string(out)) != request.ExpectedObject {
			return errors.New("This tag changed. Refresh the tag list")
		}
		switch request.Action {
		case "delete":
			_, err = c.run(ctx, root, c.options.LocalTimeout, "update-ref", "-d", ref, request.ExpectedObject)
		case "rename":
			if err = c.validateTag(ctx, root, request.NewName); err != nil {
				return err
			}
			commands := "start\ncreate refs/tags/" + request.NewName + " " + request.ExpectedObject + "\ndelete " + ref + " " + request.ExpectedObject + "\nprepare\ncommit\n"
			_, err = c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, []byte(commands), "update-ref", "--stdin")
		case "push":
			remote, e := c.selectRemote(ctx, root, request.Remote)
			if e != nil {
				return e
			}
			if e = validateRemoteURL(remote.pushSource); e != nil {
				return e
			}
			_, err = c.authenticatedRun(ctx, root, c.options.RemoteTimeout, remote.pushSource, "push", "--porcelain", "--", remote.Name, ref+":"+ref)
		default:
			return errors.New("Choose create, rename, delete, or push")
		}
		return err
	})
}
func (c *Client) validateRemoteName(ctx context.Context, root, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\x00\r\n ") {
		return errors.New("Choose a valid remote name")
	}
	return c.validateBranch(ctx, root, "remote/"+name)
}
func (c *Client) MutateRemote(ctx context.Context, path string, request RemoteRequest) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.validateRemoteName(ctx, root, request.Name); err != nil {
			return err
		}
		if request.Action == "add" {
			if err := validateRemoteURL(request.URL); err != nil {
				return err
			}
			_, err := c.run(ctx, root, c.options.LocalTimeout, "remote", "add", "--", request.Name, request.URL)
			return err
		}
		remotes, err := c.remotes(ctx, root)
		if err != nil {
			return err
		}
		found := false
		for _, remote := range remotes {
			if remote.Name == request.Name {
				found = true
				if remote.FetchURL != request.ExpectedFetchURL || remote.PushURL != request.ExpectedPushURL {
					return errors.New("This remote changed. Refresh before editing")
				}
			}
		}
		if !found {
			return errors.New("This remote no longer exists")
		}
		switch request.Action {
		case "url", "push-url":
			if err = validateRemoteURL(request.URL); err != nil {
				return err
			}
			args := []string{"remote", "set-url"}
			if request.Action == "push-url" {
				args = append(args, "--push")
			}
			_, err = c.run(ctx, root, c.options.LocalTimeout, append(args, "--", request.Name, request.URL)...)
		case "rename":
			if err = c.validateRemoteName(ctx, root, request.NewName); err != nil {
				return err
			}
			_, err = c.run(ctx, root, c.options.LocalTimeout, "remote", "rename", "--", request.Name, request.NewName)
		case "remove":
			_, err = c.run(ctx, root, c.options.LocalTimeout, "remote", "remove", "--", request.Name)
		default:
			return errors.New("Choose a remote action")
		}
		return err
	})
}
func (c *Client) SigningConfig(ctx context.Context, path string) (SigningConfig, error) {
	config := SigningConfig{Format: "openpgp"}
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		for key, target := range map[string]*string{"gpg.format": &config.Format, "user.signingkey": &config.Key} {
			out, err := c.run(ctx, root, c.options.LocalTimeout, "config", "--get", key)
			if err != nil && !isExitCode(err, 1) {
				return err
			}
			if err == nil {
				*target = strings.TrimSpace(string(out))
			}
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "config", "--type=bool", "--get", "commit.gpgsign")
		if err != nil && !isExitCode(err, 1) {
			return err
		}
		config.Enabled = strings.TrimSpace(string(out)) == "true"
		return nil
	})
	return config, err
}
func (c *Client) SaveSigningConfig(ctx context.Context, path string, config SigningConfig) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if config.Format != "openpgp" && config.Format != "ssh" && config.Format != "x509" {
			return errors.New("Choose OpenPGP, SSH, or X.509 signing")
		}
		if len(config.Key) > 8192 || strings.ContainsAny(config.Key, "\x00\r\n") || config.Enabled && strings.TrimSpace(config.Key) == "" {
			return errors.New("Choose a signing key")
		}
		values := [][2]string{{"gpg.format", config.Format}, {"user.signingkey", config.Key}, {"commit.gpgsign", "false"}}
		if config.Enabled {
			values[2][1] = "true"
		}
		for _, value := range values {
			if _, err := c.run(ctx, root, c.options.LocalTimeout, "config", "--local", "--replace-all", value[0], value[1]); err != nil {
				return err
			}
		}
		return nil
	})
}
func (c *Client) CommitSignature(ctx context.Context, path, sha string) (CommitSignature, error) {
	var result CommitSignature
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if !objectID.MatchString(sha) {
			return errors.New("Choose a full commit ID")
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "log", "-1", "--format=%G?%x00%GS%x00%GK", sha, "--")
		if err != nil {
			return err
		}
		fields := strings.Split(strings.TrimSuffix(string(out), "\n"), "\x00")
		if len(fields) != 3 {
			return errors.New("Invalid signature result")
		}
		result = CommitSignature{fields[0], fields[1], fields[2]}
		return nil
	})
	return result, err
}
