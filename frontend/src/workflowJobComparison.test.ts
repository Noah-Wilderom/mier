import assert from "node:assert/strict";
import test from "node:test";
import type { WorkflowJob } from "./actionsTypes.ts";
import { compareWorkflowJobs } from "./workflowJobComparison.ts";

const job = (id: number, name: string, seconds: number): WorkflowJob => ({
  id,
  runId: 1,
  runAttempt: 1,
  name,
  status: "completed",
  conclusion: "success",
  startedAt: "2026-10-17T00:00:00Z",
  completedAt: new Date(
    Date.parse("2026-10-17T00:00:00Z") + seconds * 1000,
  ).toISOString(),
  url: "",
  checkRunUrl: "",
  runnerName: "",
  steps: [],
});

test("run comparisons preserve every repeated job without pairing by array order", () => {
  const groups = compareWorkflowJobs(
    [job(1, "Tests", 10), job(2, "Tests", 40), job(3, "Build", 30)],
    [job(7, "Tests", 50), job(8, "Build", 42), job(9, "Tests", 5)],
  );
  assert.equal(groups.length, 2);
  assert.deepEqual(
    groups[0].before.map((value) => value.id),
    [1, 2],
  );
  assert.deepEqual(
    groups[0].after.map((value) => value.id),
    [7, 9],
  );
  assert.equal(groups[0].ambiguous, true);
  assert.equal(groups[0].durationChange, undefined);
  assert.equal(groups[1].durationChange, 12);
});

test("missing, running and invalid-timestamp jobs do not report invented duration changes", () => {
  const groups = compareWorkflowJobs(
    [job(1, "Removed", 5), job(2, "Running", 30), job(3, "Invalid", 5)],
    [
      job(4, "Added", 10),
      { ...job(5, "Running", 0), completedAt: "" },
      { ...job(6, "Invalid", 0), completedAt: "bad date" },
    ],
  );
  assert.equal(groups.length, 4);
  assert.deepEqual(groups[0].after, []);
  assert.deepEqual(groups[3].before, []);
  assert.ok(groups.every((value) => value.durationChange === undefined));
});
