package main

import (
	"context"
	"encoding/json"
	"mag-ik-een-review/internal/reviewagent"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAIReadsLinkedJiraAndLinearTicketsWithoutSearchOrMutation(t *testing.T) {
	jira, linear := newJiraHarness(t), newLinearHarness(t)
	jira.authorize(t)
	linear.authorize(t)
	ctx := context.Background()
	request := fixtureJiraPR(12)
	request.Title += " ENG-7 ENG-8 OTHER-1"
	if err := jira.service.SaveJiraRepositoryPreferences(ctx, request.Repository, JiraRepositoryPreferences{SiteID: "site-a", Projects: []string{"FAN"}, AcceptanceField: "customfield_10001"}); err != nil {
		t.Fatal(err)
	}
	if err := linear.service.SaveLinearRepositoryPreferences(ctx, request.Repository, LinearRepositoryPreferences{Teams: []string{"ENG"}}); err != nil {
		t.Fatal(err)
	}
	for _, selection := range []TicketSelection{{PullRequest: request, Key: "FAN-7", Action: "pin"}, {PullRequest: request, Key: "FAN-19", Action: "hide"}} {
		if err := jira.service.SelectJiraTicket(ctx, selection); err != nil {
			t.Fatal(err)
		}
	}
	for _, selection := range []TicketSelection{{PullRequest: request, Key: "ENG-9", Action: "pin"}, {PullRequest: request, Key: "ENG-8", Action: "hide"}} {
		if err := linear.service.SelectLinearTicket(ctx, selection); err != nil {
			t.Fatal(err)
		}
	}
	jira.mu.Lock()
	jira.calls = nil
	jira.mu.Unlock()
	linear.mu.Lock()
	linear.queries = nil
	linear.mu.Unlock()
	service := &AIService{jira: jira.service, linear: linear.service}
	references, reader, warnings := service.captureReviewTickets(ctx, request)
	want := []reviewagent.TicketReference{{Provider: "jira", Key: "FAN-7", Sources: []string{"manual"}}, {Provider: "jira", Key: "FAN-2187", Sources: []string{"branch", "title"}}, {Provider: "jira", Key: "FAN-404", Sources: []string{"description"}}, {Provider: "linear", Key: "ENG-9", Sources: []string{"manual"}}, {Provider: "linear", Key: "ENG-7", Sources: []string{"title"}}}
	if !reflect.DeepEqual(references, want) || len(warnings) != 0 {
		t.Fatal("incorrect linked ticket scope", references, warnings)
	}
	if len(jira.calls) != 0 || len(linear.queries) != 0 {
		t.Fatal("capture should not fetch any ticket content")
	}
	for _, ref := range []reviewagent.TicketReference{references[0], references[4]} {
		content, err := reader(ctx, ref.Provider, ref.Key)
		if err != nil || content.Provider != ref.Provider || content.Key != ref.Key || content.Description == "" || len(content.Comments) != 1 || content.ReadAt == "" {
			t.Fatal("missing ticket content", content, err)
		}
		if ref.Provider == "jira" && (content.Description != "Keep the original source unchanged." || content.AcceptanceCriteria != "Hover and definitions work.") {
			t.Fatal("ADF/acceptance criteria not readable", content)
		}
		if ref.Provider == "linear" && !strings.Contains(content.Description, "## Requirements") {
			t.Fatal("Linear Markdown was lost", content)
		}
		data, _ := json.Marshal(content)
		if strings.Contains(string(data), "fixture-access") || strings.Contains(string(data), "fixture-refresh") || strings.Contains(string(data), "fixture-client-secret") {
			t.Fatal("credentials exposed in ticket context")
		}
	}
	before := len(jira.calls) + len(linear.queries)
	for _, key := range []string{"FAN-19", "FAN-99", "OTHER-1"} {
		if _, err := reader(ctx, "jira", key); err == nil {
			t.Fatal("hidden/unlinked ticket readable", key)
		}
	}
	if _, err := reader(ctx, "linear", "ENG-8"); err == nil {
		t.Fatal("hidden Linear ticket readable")
	}
	if len(jira.calls)+len(linear.queries) != before {
		t.Fatal("unlinked requests reached providers")
	}
	if _, err := reader(ctx, "jira", "FAN-404"); err == nil || strings.Contains(err.Error(), "fixture-") {
		t.Fatal("missing ticket error exposed provider response", err)
	}
	jira.mu.Lock()
	for _, path := range jira.calls {
		if strings.Contains(path, "search") {
			t.Fatal("AI searched arbitrary tickets")
		}
	}
	jira.mu.Unlock()
	linear.mu.Lock()
	for _, query := range linear.queries {
		if strings.Contains(query, "mutation") || strings.Contains(query, "Search") {
			t.Fatal("AI modified or searched tickets")
		}
	}
	linear.mu.Unlock()
	// Changing personal selections does not expand an existing run's scope.
	if err := jira.service.SelectJiraTicket(ctx, TicketSelection{PullRequest: request, Key: "FAN-99", Action: "pin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader(ctx, "jira", "FAN-99"); err == nil {
		t.Fatal("ticket allowlist expanded after review start")
	}
	// Epoch checks also apply to content already present in provider caches.
	if err := jira.service.DisconnectJira(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := reader(ctx, "jira", "FAN-7"); err == nil {
		t.Fatal("disconnected Jira cache remained accessible")
	}
	if err := linear.service.SaveLinearRepositoryPreferences(ctx, request.Repository, LinearRepositoryPreferences{Teams: []string{"OTHER"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader(ctx, "linear", "ENG-7"); err == nil {
		t.Fatal("changed Linear scope remained accessible")
	}
}

func TestAITicketUnavailableProvidersAndCommentsDoNotBlockOtherContext(t *testing.T) {
	jira, linear := newJiraHarness(t), newLinearHarness(t)
	service := &AIService{jira: jira.service, linear: linear.service}
	request := fixtureJiraPR(12)
	refs, _, warnings := service.captureReviewTickets(context.Background(), request)
	if len(refs) != 0 || len(warnings) != 0 {
		t.Fatal("disconnected providers exposed ticket context")
	}
	jira.authorize(t)
	jira.handler = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/comment") {
			w.WriteHeader(500)
			return true
		}
		if strings.Contains(r.URL.Path, "/issue/") && r.Method != http.MethodGet {
			t.Error("AI ticket reads must not write to Jira")
		}
		return false
	}
	refs, reader, _ := service.captureReviewTickets(context.Background(), request)
	if len(refs) != 3 {
		t.Fatal("connected Jira references missing", refs)
	}
	content, err := reader(context.Background(), "jira", "FAN-2187")
	if err != nil || content.Description == "" || len(content.Warnings) != 1 {
		t.Fatal("comment failure prevented description read", content, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader(ctx, "jira", "FAN-2187"); err == nil {
		t.Fatal("cancelled request returned cached content")
	}
}

func TestAITicketTextPreservesVisibleADFAndBoundsUntrustedContent(t *testing.T) {
	var document any
	if err := json.Unmarshal([]byte(`{"type":"doc","attrs":{"secret":"hidden-attribute"},"content":[{"type":"heading","content":[{"type":"text","text":"Requirements"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Read "},{"type":"mention","attrs":{"text":"@Reviewer","id":"hidden-id"}},{"type":"hardBreak"},{"type":"text","text":"Keep source unchanged","marks":[{"type":"link","attrs":{"href":"hidden-url"}}]}]}]}]}]}`), &document); err != nil {
		t.Fatal(err)
	}
	content := aiTicketContent("jira", TicketIssue{Key: "FAN-7", Description: document})
	if content.Description != "Requirements\n- Read @Reviewer\nKeep source unchanged" || content.Truncated || strings.Contains(content.Description, "hidden-") {
		t.Fatal("bad ADF conversion", content)
	}
	deep := any(map[string]any{"type": "text", "text": "deep"})
	for i := 0; i < 50; i++ {
		deep = map[string]any{"type": "doc", "content": []any{deep}}
	}
	content = aiTicketContent("jira", TicketIssue{Key: "FAN-7", Description: deep})
	if !content.Truncated {
		t.Fatal("unbounded ADF traversal")
	}
	issue := TicketIssue{Key: "ENG-7", Summary: strings.Repeat("α", 10000), Description: strings.Repeat("λ", 20000), AcceptanceCriteria: strings.Repeat("β", 20000), Labels: []string{strings.Repeat("x", 1000)}}
	for i := 0; i < 20; i++ {
		issue.Comments = append(issue.Comments, TicketComment{Author: strings.Repeat("a", 1000), Body: strings.Repeat("γ", 4000)})
	}
	content = aiTicketContent("linear", issue)
	data, err := json.Marshal(content)
	if err != nil || !utf8.Valid(data) || !content.Truncated || content.TextBytes() > reviewagent.MaxTicketTextBytes || len(content.Comments) > 10 || len(content.Warnings) == 0 {
		t.Fatal("ticket limits not enforced", content.TextBytes(), err)
	}
}
