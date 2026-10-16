package main

import (
	"context"
	"errors"
	"fmt"
	"mag-ik-een-review/internal/typesafe"
	"strings"
)

func (s *PredictionService) prepareFindings(ctx context.Context, r PredictionRequest, questions map[string]typesafe.Question, rows []PredictionRow, warnings []string) (typesafe.Request, []PredictionRow, []string, error) {
	s.ai.mu.Lock()
	task := s.ai.tasks[r.RunID]
	s.ai.mu.Unlock()
	if task == nil || task.run == nil {
		return typesafe.Request{}, nil, nil, errors.New("Open an existing AI review before triaging findings")
	}
	capture := task.run.Context()
	snapshot := task.run.Snapshot()
	if capture.Host != r.Scope.Host || capture.Owner != r.Scope.Owner || capture.Repository != r.Scope.Repo || capture.PullRequest != r.PullRequest || capture.HeadSHA != r.HeadSHA || capture.BaseSHA != r.BaseSHA {
		return typesafe.Request{}, nil, nil, errors.New("These findings belong to a different revision. Start a new review first")
	}
	if snapshot.State != "completed" && snapshot.State != "cancelled" && snapshot.State != "failed" {
		return typesafe.Request{}, nil, nil, errors.New("Finish or stop the AI review before triaging its findings")
	}
	findings := []map[string]any{}
	patches := map[string]string{}
	remaining := 12 * 1024
	for _, finding := range snapshot.Findings {
		if len(findings) >= 12 {
			warnings = append(warnings, "Only the first 12 findings were assessed. Remaining findings are still available.")
			break
		}
		for _, file := range capture.Files {
			if file.Path == finding.Path && patches[file.Path] == "" {
				patch := predictionText(file.Patch, min(2048, remaining))
				remaining -= len(patch)
				patches[file.Path] = patch
				if len(patch) < len(file.Patch) {
					warnings = append(warnings, "Partial source evidence: "+file.Path)
				}
				break
			}
		}
		index := len(findings)
		if len(finding.Body) > 700 {
			warnings = append(warnings, fmt.Sprintf("Finding text truncated: %s:%d", finding.Path, finding.Line))
		}
		findings = append(findings, map[string]any{"id": finding.ID, "path": finding.Path, "line": finding.Line, "side": finding.Side, "body": predictionText(finding.Body, 700)})
		row := PredictionRow{ID: fmt.Sprintf("finding_%d_evidence", index), Label: "Source support", Path: finding.Path, Line: finding.Line, Side: finding.Side, FindingID: finding.ID}
		predictionQuestion(questions, &rows, row, predictionChoice(fmt.Sprintf("How well does state.patches support the concrete claim in state.findings[%d]? A plausible claim without the needed code is needs_evidence. Assess the claim, not its writing style.", index), map[string]string{"supported": "The supplied code directly supports the concrete claim", "needs_evidence": "More source or contract evidence is needed", "contradicted": "The supplied evidence contradicts the concrete claim"}))
		row.ID = fmt.Sprintf("finding_%d_duplicate", index)
		row.Label = "Possible duplicate"
		predictionQuestion(questions, &rows, row, typesafe.Question{Type: "noul", Instructions: fmt.Sprintf("Does another item in state.findings describe the same underlying issue and fix as state.findings[%d]? Similar wording or a shared file alone is insufficient.", index)})
	}
	if len(findings) == 0 {
		return typesafe.Request{}, nil, nil, errors.New("This review has no findings to triage")
	}
	warnings = append(warnings, "Evidence is limited to the captured diff. All original findings remain available regardless of the assessment.")
	return typesafe.Request{State: map[string]any{"findings": findings, "patches": patches, "headSha": capture.HeadSHA, "baseSha": capture.BaseSHA}, Questions: questions}, rows, warnings, nil
}

func (s *PredictionService) prepareTicketQuestions(ctx context.Context, r PredictionRequest, branch, title, body string, state map[string]any, questions map[string]typesafe.Question, rows *[]PredictionRow, warnings *[]string) error {
	refs, read, notes := s.ai.captureReviewTickets(ctx, TicketPullRequest{Repository: TicketRepository{Host: r.Scope.Host, Owner: r.Scope.Owner, Repo: r.Scope.Repo}, Number: r.PullRequest, Branch: branch, Title: title, Body: body})
	*warnings = append(*warnings, notes...)
	tickets := []map[string]string{}
	criteria := []map[string]string{}
	for i, ref := range refs {
		if i >= 4 {
			*warnings = append(*warnings, "Ticket coverage is limited to four linked tickets per assessment.")
			break
		}
		ticket, err := read(ctx, ref.Provider, ref.Key)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			*warnings = append(*warnings, "Ticket unavailable: "+ref.Provider+" "+ref.Key)
			continue
		}
		tickets = append(tickets, map[string]string{"provider": ref.Provider, "key": ref.Key, "summary": predictionText(ticket.Summary, 250), "description": predictionText(ticket.Description, 600), "updatedAt": ticket.UpdatedAt})
		text := ticket.AcceptanceCriteria
		if strings.TrimSpace(text) == "" {
			*warnings = append(*warnings, "No acceptance criteria field for "+ref.Key+"; use the ticket intent row for a broad relevance check.")
			text = ticket.Summary
		}
		for _, line := range predictionCriteria(text) {
			if len(criteria) >= 12 {
				*warnings = append(*warnings, "Only the first 12 requirements were assessed.")
				break
			}
			index := len(criteria)
			criteria = append(criteria, map[string]string{"provider": ref.Provider, "key": ref.Key, "requirement": line})
			predictionQuestion(questions, rows, PredictionRow{ID: fmt.Sprintf("criterion_%d", index), Label: line, TicketProvider: ref.Provider, TicketKey: ref.Key}, predictionChoice(fmt.Sprintf("Based only on the supplied patches, how does state.criteria[%d].requirement relate to this change? Missing code, dependencies or tests means verify. This is not proof of completed acceptance criteria.", index), map[string]string{"appears_addressed": "The supplied changes visibly address this requirement; verification remains necessary", "verify": "Insufficient evidence or testing to judge coverage", "not_visible": "This relevant requirement has no visible implementation in the supplied changes", "not_applicable": "This requirement is outside this change's scope"}))
		}
		if ticket.Truncated {
			*warnings = append(*warnings, "Ticket content was truncated: "+ref.Key)
		}
	}
	if len(criteria) == 0 {
		return errors.New("No readable linked ticket requirements. Connect Jira or Linear and link a ticket first")
	}
	state["tickets"] = tickets
	state["criteria"] = criteria
	*warnings = append(*warnings, "Ticket content is read only and can change independently of the captured commits.")
	return nil
}
func predictionCriteria(text string) []string {
	result := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*• ")
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "[ ]"), "[x]"))
		if len(line) < 8 || strings.HasPrefix(line, "#") {
			continue
		}
		result = append(result, predictionText(line, 500))
		if len(result) >= 12 {
			break
		}
	}
	return result
}

func (s *PredictionService) prepareConflict(ctx context.Context, r PredictionRequest, questions map[string]typesafe.Question, rows []PredictionRow, warnings []string) (typesafe.Request, []PredictionRow, []string, error) {
	if r.Scope.Root == "" || r.ConflictToken == "" {
		return typesafe.Request{}, nil, nil, errors.New("Open a current local conflict first")
	}
	file, err := s.repositories.Conflict(ctx, r.Scope.Root, r.Path)
	if err != nil {
		return typesafe.Request{}, nil, nil, err
	}
	if file.Token != r.ConflictToken {
		return typesafe.Request{}, nil, nil, errors.New("This conflict changed. Reload it before asking Jev")
	}
	if !file.Editable {
		return typesafe.Request{}, nil, nil, errors.New("Jev conflict guidance supports editable text conflicts only")
	}
	state := map[string]any{"path": file.Path, "base": predictionText(file.Base.Content, 5000), "current": predictionText(file.Current.Content, 5000), "incoming": predictionText(file.Incoming.Content, 5000), "baseExists": file.Base.Exists, "currentExists": file.Current.Exists, "incomingExists": file.Incoming.Exists}
	if len(file.Base.Content) > 5000 || len(file.Current.Content) > 5000 || len(file.Incoming.Content) > 5000 {
		warnings = append(warnings, "Conflict versions are truncated to 5,000 bytes each; omitted behavior requires manual inspection.")
	}
	predictionQuestion(questions, &rows, PredictionRow{ID: "conflict_route", Label: "Suggested next step", Path: file.Path}, predictionChoice("What next step best preserves both sides' intended behavior given the supplied base/current/incoming versions? Route ambiguous semantic conflicts to manual inspection or agent investigation. This selects an investigation path, never a correct resolution.", map[string]string{"manual": "Inspect the disagreement manually", "agent": "Ask the existing coding assistant to investigate the surrounding contracts", "compare_current": "Inspect whether the current side already preserves the incoming intent", "compare_incoming": "Inspect whether the incoming side already preserves the current intent", "combine": "Inspect how both independent changes can be combined"}))
	predictionQuestion(questions, &rows, PredictionRow{ID: "conflict_semantics", Label: "Behavior disagreement", Path: file.Path}, typesafe.Question{Type: "noul", Instructions: "Do the current and incoming versions appear to disagree about intended runtime behavior rather than only formatting or location?"})
	warnings = append(warnings, "This assessment cannot change your result, stage a file or continue a Git operation.")
	return typesafe.Request{State: state, Questions: questions}, rows, warnings, nil
}

func (s *PredictionService) prepareCI(ctx context.Context, r PredictionRequest, questions map[string]typesafe.Question, rows []PredictionRow, warnings []string) (typesafe.Request, []PredictionRow, []string, error) {
	if r.Scope.Root != "" || r.JobID < 1 {
		return typesafe.Request{}, nil, nil, errors.New("Open a completed GitHub Actions job first")
	}
	log, err := s.github.GetWorkflowJobLog(ctx, r.Scope.Host, r.Scope.Owner, r.Scope.Repo, r.JobID)
	if err != nil {
		return typesafe.Request{}, nil, nil, err
	}
	if log.State != "available" || strings.TrimSpace(log.Text) == "" {
		return typesafe.Request{}, nil, nil, errors.New("This job has no available log to assess")
	}
	excerpts := predictionLogExcerpts(log.Text)
	state := map[string]any{"job": log.Job.Name, "conclusion": log.Job.Conclusion, "excerpts": excerpts}
	predictionQuestion(questions, &rows, PredictionRow{ID: "ci_category", Label: "Likely failure category"}, predictionChoice("What is the primary cause category supported by state.excerpts? Classify the evidence; do not infer a cause from the final nonzero exit code alone. Use unknown when the root cause is not visible.", map[string]string{"tests": "A test assertion, test crash or test timeout", "build": "Compiler, type checker, linter or packaging error", "dependency": "Dependency resolution, installation or version mismatch", "permissions": "Credentials, authorization or access failure", "infrastructure": "Runner, network or external service failure", "configuration": "Workflow setup, inputs, paths or environment configuration", "unknown": "Insufficient evidence or another cause"}))
	for i, excerpt := range excerpts {
		predictionQuestion(questions, &rows, PredictionRow{ID: fmt.Sprintf("log_%d_relevance", i), Label: fmt.Sprintf("Log evidence near line %d", excerpt.Line), Line: excerpt.Line, Excerpt: excerpt.Text}, typesafe.Question{Type: "score", Instructions: fmt.Sprintf("How useful is state.excerpts[%d] for locating the first actionable cause of this job failure? Prioritize direct error evidence over cascading failures.", i), Criteria: []string{"Unrelated or only routine progress", "Possibly relevant evidence that needs investigation", "Direct evidence of an actionable failure cause"}})
	}
	if log.Truncated {
		warnings = append(warnings, "GitHub log download is truncated; original line numbers refer to the available downloaded text.")
	}
	warnings = append(warnings, "Only bounded log excerpts were sent. Predictions cannot rerun or cancel jobs.")
	return typesafe.Request{State: state, Questions: questions}, rows, warnings, nil
}

type predictionLogExcerpt struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

func predictionLogExcerpts(text string) []predictionLogExcerpt {
	lines := strings.Split(text, "\n")
	result := []predictionLogExcerpt{}
	lastEnd := -1
	for i, line := range lines {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "error") && !strings.Contains(lower, "fail") && !strings.Contains(lower, "denied") && !strings.Contains(lower, "exception") {
			continue
		}
		start, end := max(0, i-3), min(len(lines), i+5)
		if start <= lastEnd {
			continue
		}
		result = append(result, predictionLogExcerpt{Line: start + 1, Text: predictionText(strings.Join(lines[start:end], "\n"), 1200)})
		lastEnd = end - 1
		if len(result) >= 10 {
			break
		}
	}
	if len(result) == 0 {
		start := max(0, len(lines)-40)
		result = append(result, predictionLogExcerpt{Line: start + 1, Text: predictionText(strings.Join(lines[start:], "\n"), 4000)})
	}
	return result
}
