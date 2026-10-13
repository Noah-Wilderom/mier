// @ts-nocheck -- Executed directly by Node; production types are checked in data.ts.
import test from "node:test";
import assert from "node:assert/strict";
import {
  currentUser,
  latestReview,
  mergeBlockers,
  mergePullRequest,
  seedHistory,
  seedLocalChanges,
  seedPullRequests,
  submitReview,
} from "./data.ts";

function fixture(number = 142) {
  const pr = seedPullRequests().find(
    (candidate) => candidate.number === number,
  );
  assert.ok(pr, "Expected fixture #" + number);
  return pr;
}

test("seed calls return independent state with internally consistent diffs", () => {
  const first = seedPullRequests();
  const second = seedPullRequests();
  assert.equal(first.length, 8);
  assert.notEqual(first[0], second[0]);
  first[0].threads[0].comments[0].body = "Edited locally";
  first[0].files[0].lines[0].text = "Edited locally";
  assert.notEqual(
    first[0].threads[0].comments[0].body,
    second[0].threads[0].comments[0].body,
  );
  assert.notEqual(
    first[0].files[0].lines[0].text,
    second[0].files[0].lines[0].text,
  );

  for (const pr of second) {
    assert.equal(
      pr.commits[0].sha,
      pr.headSha,
      "Commit/head mismatch on #" + pr.number,
    );
    for (const file of pr.files) {
      assert.equal(
        file.additions,
        file.lines.filter((line) => line.kind === "add").length,
      );
      assert.equal(
        file.deletions,
        file.lines.filter((line) => line.kind === "remove").length,
      );
      let oldLine = 0;
      let newLine = 0;
      for (const line of file.lines) {
        if (line.oldLine !== undefined) assert.equal(line.oldLine, ++oldLine);
        if (line.newLine !== undefined) assert.equal(line.newLine, ++newLine);
      }
    }
    for (const thread of pr.threads) {
      const file = pr.files.find((candidate) => candidate.path === thread.path);
      assert.ok(file, "Thread file exists");
      assert.ok(
        file.lines.some((line) =>
          thread.side === "new"
            ? line.newLine === thread.line
            : line.oldLine === thread.line,
        ),
      );
    }
  }
  assert.equal(second[0].files.length, 6);
  assert.equal(second[0].commits.length, 4);
  assert.ok(second[0].files.every((file) => file.lines.length >= 50));
  assert.equal(seedLocalChanges.length, 3);
  assert.equal(seedLocalChanges.filter((file) => file.staged).length, 1);
  assert.equal(fixture(138).merge.sha, seedHistory[0].sha);
});

test("selected PR explains approval and conversation blockers", () => {
  const blockers = mergeBlockers(fixture());
  assert.equal(blockers.length, 2);
  assert.match(blockers[0], /1 more approval/);
  assert.match(blockers[1], /1 conversation/);
});

test("approval, thread resolution and merge form an immutable complete workflow", () => {
  const original = fixture();
  const resolved = structuredClone(original);
  resolved.threads.forEach((thread) => {
    thread.resolved = true;
  });
  const approved = submitReview(
    resolved,
    "approved",
    "Checked focus after refresh.",
  );
  assert.equal(latestReview(approved, currentUser).decision, "approved");
  assert.deepEqual(mergeBlockers(approved), []);
  assert.equal(resolved.reviews.length, 1);
  assert.equal(original.threads[0].resolved, false);

  for (const method of ["merge", "squash", "rebase"]) {
    const merged = mergePullRequest(approved, method);
    assert.equal(merged.status, "merged");
    assert.equal(merged.merge.method, method);
    assert.match(merged.merge.sha, /^[a-f0-9]{7}$/);
    assert.notEqual(merged.merge.sha, merged.headSha);
    assert.equal(approved.status, "open");
    assert.throws(
      () => mergePullRequest(merged, method),
      /already been merged/,
    );
  }
});

test("a review publishes draft inline comments with stable diff anchors", () => {
  const original = fixture();
  const target = original.files[0].lines.find((line) => line.kind === "add");
  original.pendingComments.push({
    id: "pending-1",
    path: original.files[0].path,
    line: target.newLine,
    side: "new",
    body: "  Could this keep focus on the same path?  ",
  });
  const result = submitReview(original, "commented", "");
  assert.equal(result.pendingComments.length, 0);
  assert.equal(result.threads.length, original.threads.length + 1);
  const published = result.threads[result.threads.length - 1];
  assert.equal(published.line, target.newLine);
  assert.equal(published.side, "new");
  assert.equal(
    published.comments[0].body,
    "Could this keep focus on the same path?",
  );
  assert.equal(published.comments[0].author, currentUser);
  assert.equal(published.resolved, false);
  assert.equal(original.pendingComments.length, 1);
});

test("multiple pending comments receive distinct thread and comment identifiers", () => {
  const pr = fixture();
  const line = pr.files[0].lines.find(
    (candidate) => candidate.kind === "add",
  ).newLine;
  pr.pendingComments = [
    {
      id: "pending-1",
      path: pr.files[0].path,
      side: "new",
      line,
      body: "First comment",
    },
    {
      id: "pending-2",
      path: pr.files[0].path,
      side: "new",
      line,
      body: "Second comment",
    },
  ];
  const result = submitReview(pr, "commented", "Two small questions.");
  assert.equal(
    new Set(result.threads.map((thread) => thread.id)).size,
    result.threads.length,
  );
  assert.equal(
    new Set(
      result.threads.flatMap((thread) =>
        thread.comments.map((comment) => comment.id),
      ),
    ).size,
    result.threads.flatMap((thread) => thread.comments).length,
  );
});

test("review validation prevents blank feedback, self approval and invalid anchors", () => {
  const pr = fixture();
  assert.throws(() => submitReview(pr, "changes_requested", "  "), /Explain/);
  assert.throws(
    () => submitReview(pr, "commented", " "),
    /Add a review comment/,
  );
  const own = fixture(139);
  own.status = "open";
  assert.throws(() => submitReview(own, "approved", ""), /own pull request/);

  pr.pendingComments.push({
    id: "invalid",
    path: pr.files[0].path,
    line: 10000,
    side: "new",
    body: "A question",
  });
  assert.throws(() => submitReview(pr, "commented", ""), /no longer matches/);
  pr.pendingComments[0].line = 1;
  pr.pendingComments[0].body = " ";
  assert.throws(() => submitReview(pr, "commented", ""), /cannot be empty/);
  assert.equal(pr.reviews.length, 1);
});

test("latest formal review survives a comment-only review and rejects stale heads", () => {
  const pr = fixture();
  const approved = submitReview(pr, "approved", "Looks good.");
  const commented = submitReview(
    approved,
    "commented",
    "One follow-up for a future change.",
  );
  assert.equal(latestReview(commented, currentUser).decision, "approved");
  commented.headSha = "abcdef1";
  assert.equal(latestReview(commented, currentUser), undefined);
  assert.equal(latestReview(commented, "marcus"), undefined);
  assert.ok(
    mergeBlockers(commented).some((message) =>
      /2 more approvals/.test(message),
    ),
  );
});

test("review order uses timestamps, unique people and the latest formal decision", () => {
  const pr = fixture(135);
  pr.reviews.reverse();
  pr.reviews.push({
    ...pr.reviews[0],
    id: "duplicate-event",
    createdAt: "2026-10-11T09:13:00Z",
  });
  assert.deepEqual(mergeBlockers(pr), []);
  pr.reviews.push({
    id: "request-changes",
    author: "ravi",
    decision: "changes_requested",
    headSha: pr.headSha,
    body: "Please cover the unchecked state.",
    createdAt: "2026-10-12T08:00:00Z",
  });
  assert.ok(
    mergeBlockers(pr).some((message) =>
      /Changes requested by Ravi/.test(message),
    ),
  );
  assert.ok(
    mergeBlockers(pr).some((message) => /1 more approval/.test(message)),
  );
  pr.reviews.push({
    id: "approve-later",
    author: "ravi",
    decision: "approved",
    headSha: pr.headSha,
    body: "The new coverage addresses it.",
    createdAt: "2026-10-13T08:00:00Z",
  });
  assert.deepEqual(mergeBlockers(pr), []);
});

test("required CI, conflicts and lifecycle states block merge independently", () => {
  assert.ok(
    mergeBlockers(fixture(137)).some((message) =>
      /Required checks failed/.test(message),
    ),
  );
  assert.ok(
    mergeBlockers(fixture(137)).some((message) =>
      /Changes requested/.test(message),
    ),
  );
  assert.ok(
    mergeBlockers(fixture(141)).some((message) =>
      /checks are running/.test(message),
    ),
  );
  assert.ok(
    mergeBlockers(fixture(131)).some((message) =>
      /merge conflicts/.test(message),
    ),
  );
  assert.ok(
    mergeBlockers(fixture(139)).some((message) =>
      /ready for review/.test(message),
    ),
  );
  assert.deepEqual(mergeBlockers(fixture(134)), [
    "Reopen this pull request before merging.",
  ]);
  assert.deepEqual(mergeBlockers(fixture(138)), [
    "This pull request has already been merged.",
  ]);
  assert.throws(
    () => submitReview(fixture(134), "commented", "A follow-up"),
    /closed or merged/,
  );
  assert.throws(
    () => submitReview(fixture(139), "changes_requested", "A formal review"),
    /ready before/,
  );
  assert.throws(() => mergePullRequest(fixture(), "merge"), /approval/);
});

test("optional checks do not block merge, while a required failed check does", () => {
  const pr = fixture(135);
  pr.checks.find((check) => !check.required).status = "failed";
  assert.deepEqual(mergeBlockers(pr), []);
  pr.checks[0].status = "failed";
  assert.throws(() => mergePullRequest(pr, "squash"), /Required checks failed/);
});
