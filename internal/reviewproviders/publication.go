package reviewproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mag-ik-een-review/internal/githubclient"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Publisher struct {
	mu     sync.Mutex
	store  ProgressStore
	memory map[string]PublicationProgress
}

func NewPublisher(store ProgressStore) *Publisher {
	return &Publisher{store: store, memory: map[string]PublicationProgress{}}
}

type publicationActions struct {
	Find    func(context.Context, string) (map[string]PublishedComment, error)
	Comment func(context.Context, githubclient.ReviewCommentRequest) (githubclient.Comment, error)
	Body    func(context.Context, string) (githubclient.Comment, error)
	Vote    func(context.Context, string) error
}

var publicationSuffix = regexp.MustCompile(`\n\n<!-- mier-review:([a-f0-9]{64}):([a-f0-9]{64}):([a-f0-9]{64}) -->$`)

func cleanBody(body string) string { return publicationSuffix.ReplaceAllString(body, "") }
func publicationMarker(body, key string) (string, PublishedComment, bool) {
	match := publicationSuffix.FindStringSubmatch(body)
	if len(match) != 4 || match[1] != key {
		return "", PublishedComment{}, false
	}
	return match[2], PublishedComment{Hash: match[3]}, true
}
func markedBody(body, key, item, fingerprint string) string {
	return body + "\n\n<!-- mier-review:" + key + ":" + item + ":" + fingerprint + " -->"
}
func validateReview(input githubclient.ReviewRequest, requestChanges bool) error {
	if !immutable(input.CommitID) {
		return errors.New("Review requires the displayed head commit")
	}
	if input.Event != "COMMENT" && input.Event != "APPROVE" && input.Event != "REQUEST_CHANGES" {
		return errors.New("Choose comment, approve, or request changes")
	}
	if input.Event == "REQUEST_CHANGES" && !requestChanges {
		return errors.New("This provider does not expose a supported request-changes operation; leave a comment instead")
	}
	if len(input.Comments) > 100 || len(input.Body) > 100*1024 {
		return errors.New("A review supports up to 100 comments and a 100 KiB summary")
	}
	seen := map[string]bool{}
	for _, comment := range input.Comments {
		if comment.ClientID != "" {
			if seen[comment.ClientID] {
				return errors.New("Each draft comment must have its own ID")
			}
			seen[comment.ClientID] = true
		}
		if !validFile(comment.Path) || comment.Line < 1 || (comment.Side != "LEFT" && comment.Side != "RIGHT") || strings.TrimSpace(comment.Body) == "" || len(comment.Body) > 100*1024 || len(comment.ClientID) > 256 {
			return errors.New("Each review comment needs a valid path, line, side, and body")
		}
		if comment.CommitID != "" && comment.CommitID != input.CommitID {
			return errors.New("Every comment must belong to the reviewed head")
		}
	}
	if input.Event == "COMMENT" && strings.TrimSpace(input.Body) == "" && len(input.Comments) == 0 {
		return errors.New("Write a review summary or add a comment")
	}
	return nil
}
func (p *Publisher) publish(ctx context.Context, scope string, input githubclient.ReviewRequest, user githubclient.User, actions publicationActions) (githubclient.Review, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := hash(scope, input.CommitID, user.Login)
	progress := p.memory[key]
	if p.store != nil {
		var err error
		progress, err = p.store.Load(key)
		if err != nil {
			return githubclient.Review{}, fmt.Errorf("Read saved review progress: %w", err)
		}
	}
	if progress.Comments == nil {
		progress.Comments = map[string]PublishedComment{}
	}
	if progress.Votes == nil {
		progress.Votes = map[string]bool{}
	}
	if progress.StartedAt == "" {
		progress.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	save := func() error {
		p.memory[key] = progress
		if p.store != nil {
			return p.store.Save(key, progress)
		}
		return nil
	}
	if err := save(); err != nil {
		return githubclient.Review{}, err
	}
	// Server markers recover a successful response lost to a disconnect or an
	// interrupted local save. Draft IDs stay stable when other drafts are edited.
	existing, err := actions.Find(ctx, key)
	if err != nil {
		return githubclient.Review{}, err
	}
	for item, posted := range existing {
		if old, ok := progress.Comments[item]; ok && old.Hash != posted.Hash {
			return githubclient.Review{}, errors.New("Published review progress conflicts with the server; refresh this review")
		}
		progress.Comments[item] = posted
	}
	posted := 0
	fail := func(err error) (githubclient.Review, error) {
		return githubclient.Review{}, fmt.Errorf("Review paused after %d comment(s) in this attempt. Published comments are kept; retry continues the remaining review: %w", posted, err)
	}
	for index, comment := range input.Comments {
		if err = ctx.Err(); err != nil {
			return fail(err)
		}
		clientID := comment.ClientID
		if clientID == "" {
			clientID = hash(comment.Body, comment.Path, strconv.Itoa(comment.Line), comment.Side)
		}
		item := hash("comment", clientID)
		encoded, _ := json.Marshal(comment)
		fingerprint := hash(string(encoded), input.CommitID)
		if prior, ok := progress.Comments[item]; ok {
			if prior.Hash != fingerprint {
				return fail(errors.New("A draft comment was already published with different content. Add a new comment to revise it"))
			}
			continue
		}
		comment.CommitID = input.CommitID
		comment.Body = markedBody(comment.Body, key, item, fingerprint)
		result, err := actions.Comment(ctx, comment)
		if err != nil {
			return fail(fmt.Errorf("Comment %d: %w", index+1, err))
		}
		progress.Comments[item] = PublishedComment{ID: result.ID, Hash: fingerprint}
		posted++
		if err = save(); err != nil {
			return fail(err)
		}
	}
	if strings.TrimSpace(input.Body) != "" {
		item := hash("body", input.Body)
		if _, ok := progress.Comments[item]; !ok {
			fingerprint := hash(input.Body)
			comment, err := actions.Body(ctx, markedBody(input.Body, key, item, fingerprint))
			if err != nil {
				return fail(err)
			}
			progress.Comments[item] = PublishedComment{ID: comment.ID, Hash: fingerprint}
			posted++
			if err = save(); err != nil {
				return fail(err)
			}
		}
	}
	if progress.LastVote == "" && len(progress.Votes) == 1 {
		for vote := range progress.Votes {
			progress.LastVote = vote
		}
	}
	if input.Event != "COMMENT" && progress.LastVote != input.Event {
		if err = actions.Vote(ctx, input.Event); err != nil {
			return fail(err)
		}
		progress.Votes[input.Event] = true
		progress.LastVote = input.Event
		if err = save(); err != nil {
			return fail(err)
		}
	}
	idBytes := hash(key, input.Body, input.Event)
	id, _ := strconv.ParseUint(idBytes[:13], 16, 64)
	return githubclient.Review{ID: int64(id), NodeID: key, User: user, Body: input.Body, State: map[string]string{"COMMENT": "COMMENTED", "APPROVE": "APPROVED", "REQUEST_CHANGES": "CHANGES_REQUESTED"}[input.Event], CommitID: input.CommitID, SubmittedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}
