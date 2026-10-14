import { strict as assert } from "node:assert";
import { test } from "node:test";
import {
  aiPendingComment,
  mergePendingComments,
  type AIFinding,
  type AIReview,
} from "./aiTypes.ts";
import type { PendingComment, PullRequest } from "./data.ts";

const draft = (id: string, body = id): PendingComment => ({
  id,
  path: "file.ts",
  line: 1,
  side: "new",
  body,
});
test("human edits and deletion preserve asynchronously received AI drafts", () => {
  assert.deepEqual(
    mergePendingComments(
      [draft("human")],
      [draft("human", "edited")],
      [draft("human"), draft("ai")],
    ),
    [draft("human", "edited"), draft("ai")],
  );
  assert.deepEqual(
    mergePendingComments([draft("ai")], [], [draft("ai"), draft("human")]),
    [draft("human")],
  );
  assert.deepEqual(mergePendingComments([], [], [draft("newer")]), [
    draft("newer"),
  ]);
});
test("AI comments retain attribution and immutable anchors and become outdated on another revision", () => {
  const run = {
    id: "run-1",
    provider: "codex",
    model: "selected-model",
    headSha: "old-head",
  } as AIReview;
  const finding: AIFinding = {
    id: "finding-1",
    path: "file.ts",
    line: 1,
    side: "new",
    body: "The retry can repeat a write.",
    severity: "bug",
  };
  const pr = {
    headSha: "old-head",
    files: [
      {
        path: "file.ts",
        lines: [{ kind: "add", newLine: 1, text: "write()" }],
      },
    ],
  } as PullRequest;
  const comment = aiPendingComment(run, finding, pr);
  assert.equal(comment.id, "ai:run-1:finding-1");
  assert.equal(comment.outdated, false);
  assert.match(comment.body, /AI review · Codex · bug/);
  assert.equal(comment.ai?.model, "selected-model");
  assert.equal(
    aiPendingComment(run, finding, { ...pr, headSha: "new-head" }).outdated,
    true,
  );
  assert.equal(
    aiPendingComment(run, { ...finding, line: 2 }, pr).outdated,
    true,
  );
});
