package reviewproviders

import (
	"context"
	"errors"
	"mag-ik-een-review/internal/githubclient"
	"strings"
	"testing"
)

func TestReviewRetryDoesNotDuplicatePublishedCommentsAfterLostResponse(t *testing.T) {
	publisher := NewPublisher(nil)
	request := githubclient.ReviewRequest{CommitID: strings.Repeat("a", 40), Event: "APPROVE", Body: "Review summary", Comments: []githubclient.ReviewCommentRequest{{ClientID: "first", Body: "First comment", Path: "file.go", Line: 1, Side: "RIGHT"}, {ClientID: "second", Body: "Second comment", Path: "file.go", Line: 2, Side: "RIGHT"}}}
	if err := validateReview(request, false); err != nil {
		t.Fatal(err)
	}
	server := map[string]PublishedComment{}
	calls, bodies, votes := 0, 0, 0
	disconnect := true
	var scopeKey string
	actions := publicationActions{Find: func(ctx context.Context, key string) (map[string]PublishedComment, error) {
		scopeKey = key
		copy := map[string]PublishedComment{}
		for key, value := range server {
			copy[key] = value
		}
		return copy, nil
	}, Comment: func(ctx context.Context, input githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
		calls++
		item, posted, ok := publicationMarker(input.Body, scopeKey)
		if !ok {
			t.Fatal("missing retry marker")
		}
		if cleanBody(input.Body) != map[string]string{"first": "First comment", "second": "Second comment"}[input.ClientID] {
			t.Fatal("body changed")
		}
		posted.ID = int64(calls)
		server[item] = posted
		if calls == 2 && disconnect {
			return githubclient.Comment{}, errors.New("response disconnected after server accepted")
		}
		return githubclient.Comment{ID: posted.ID}, nil
	}, Body: func(ctx context.Context, body string) (githubclient.Comment, error) {
		bodies++
		return githubclient.Comment{ID: 33}, nil
	}, Vote: func(ctx context.Context, event string) error { votes++; return nil }}
	if _, err := publisher.publish(context.Background(), "repo/12", request, githubclient.User{Login: "reviewer"}, actions); err == nil {
		t.Fatal("disconnect accepted")
	}
	disconnect = false
	review, err := publisher.publish(context.Background(), "repo/12", request, githubclient.User{Login: "reviewer"}, actions)
	if err != nil || calls != 2 || bodies != 1 || votes != 1 || review.State != "APPROVED" {
		t.Fatal(review, err, calls, bodies, votes)
	}
	request.Comments = []githubclient.ReviewCommentRequest{{ClientID: "third", Body: "A separate review on this head", Path: "file.go", Line: 3, Side: "RIGHT"}}
	request.Body = ""
	request.Event = "COMMENT"
	actions.Comment = func(ctx context.Context, input githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
		calls++
		return githubclient.Comment{ID: 44}, nil
	}
	if _, err = publisher.publish(context.Background(), "repo/12", request, githubclient.User{Login: "reviewer"}, actions); err != nil || calls != 3 {
		t.Fatal("new review on same head was suppressed", calls, err)
	}
}
func TestReviewRejectsEditsToAlreadyPublishedDraftAndDuplicateIDs(t *testing.T) {
	publisher := NewPublisher(nil)
	request := githubclient.ReviewRequest{CommitID: strings.Repeat("a", 40), Event: "COMMENT", Comments: []githubclient.ReviewCommentRequest{{ClientID: "one", Body: "Original", Path: "file.go", Line: 1, Side: "RIGHT"}}}
	calls := 0
	actions := publicationActions{Find: func(context.Context, string) (map[string]PublishedComment, error) { return nil, nil }, Comment: func(context.Context, githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
		calls++
		return githubclient.Comment{ID: 1}, nil
	}}
	if _, err := publisher.publish(context.Background(), "repo/12", request, githubclient.User{Login: "reviewer"}, actions); err != nil {
		t.Fatal(err)
	}
	request.Comments[0].Body = "Changed"
	if _, err := publisher.publish(context.Background(), "repo/12", request, githubclient.User{Login: "reviewer"}, actions); err == nil || calls != 1 {
		t.Fatal("already published draft edited", calls, err)
	}
	request.Comments = append(request.Comments, request.Comments[0])
	if err := validateReview(request, false); err == nil {
		t.Fatal("duplicate draft ID accepted")
	}
	request.Event = "REQUEST_CHANGES"
	if err := validateReview(request, false); err == nil {
		t.Fatal("unsupported vote accepted")
	}
}

func TestReviewCanChangeDecisionOnSameHeadWithoutResettingOnComment(t *testing.T) {
	publisher := NewPublisher(nil)
	request := githubclient.ReviewRequest{CommitID: strings.Repeat("a", 40), Event: "APPROVE"}
	user := githubclient.User{Login: "reviewer"}
	votes := []string{}
	actions := publicationActions{Find: func(context.Context, string) (map[string]PublishedComment, error) { return nil, nil }, Vote: func(_ context.Context, event string) error { votes = append(votes, event); return nil }, Body: func(context.Context, string) (githubclient.Comment, error) { return githubclient.Comment{ID: 1}, nil }}
	for _, event := range []string{"APPROVE", "APPROVE", "REQUEST_CHANGES", "COMMENT", "APPROVE"} {
		request.Event = event
		request.Body = ""
		if event == "COMMENT" {
			request.Body = "Context without changing my decision"
		}
		if _, err := publisher.publish(context.Background(), "repository/12", request, user, actions); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(votes, ",") != "APPROVE,REQUEST_CHANGES,APPROVE" {
		t.Fatalf("wrong native vote transitions: %v", votes)
	}
}
