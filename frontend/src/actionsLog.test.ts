import test from "node:test";
import assert from "node:assert/strict";
import { actionStatus, cleanJobLog, jobLogSections } from "./actionsLog.ts";

test("logs remove terminal escape codes and retain literal markup as text", () => {
  assert.equal(
    cleanJobLog(
      "\x1b[31mERROR\x1b[0m\r\n\x1b]8;;https://example.test\x07<script>literal</script>\x1b]8;;\x1b\\\x00",
    ),
    "ERROR\n<script>literal</script>",
  );
});
test("timestamped groups and errors become searchable step sections", () => {
  const sections = jobLogSections(
    "2026-10-14T01:00:00Z ##[group]Run tests\npassing\n##[error]Tests failed\n##[endgroup]\nfinal status\n",
  );
  assert.equal(sections[0].title, "Run tests");
  assert.equal(sections[0].error, true);
  assert.equal(sections[1].title, "Job output");
  assert.match(sections[1].text, /final status/);
});
test("cancelled, skipped, queued and timed out states remain distinct", () => {
  assert.equal(actionStatus("completed", "skipped").label, "Skipped");
  assert.equal(actionStatus("completed", "cancelled").label, "Cancelled");
  assert.equal(actionStatus("queued").label, "Queued");
  assert.equal(actionStatus("in_progress").label, "Running");
  assert.equal(actionStatus("completed", "timed_out").color, "text-red");
  assert.equal(actionStatus("error").color, "text-red");
});
