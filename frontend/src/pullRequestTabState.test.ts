import { strict as assert } from "node:assert";
import { test } from "node:test";
import {
  emptyPullRequestTabs,
  pullRequestTabsReducer as reduce,
} from "./pullRequestTabState.ts";

const scope = "repo-a";
test("opening an existing PR selects its one tab and keeps its own section and location", () => {
  let state = reduce(emptyPullRequestTabs(scope), {
    scope,
    type: "open",
    number: 12,
  });
  state = reduce(state, {
    scope,
    type: "section",
    number: 12,
    section: "conversation",
  });
  state = reduce(state, {
    scope,
    type: "location",
    number: 12,
    location: { path: "file.ts", line: 4, side: "new" },
  });
  state = reduce(state, { scope, type: "open", number: 13 });
  assert.equal(state.items[1].section, "changes");
  state = reduce(state, { scope, type: "open", number: 12 });
  assert.deepEqual(
    state.items.map((item) => item.number),
    [12, 13],
  );
  assert.equal(state.active, 12);
  assert.equal(state.items[0].section, "conversation");
  assert.equal(state.items[0].location?.line, 4);
});
test("closing a background tab does not select it and closing the active tab uses its neighbor", () => {
  let state = emptyPullRequestTabs(scope);
  for (const number of [12, 13, 14])
    state = reduce(state, { scope, type: "open", number });
  state = reduce(state, { scope, type: "close", number: 13 });
  assert.equal(state.active, 14);
  assert.deepEqual(
    state.items.map((item) => item.number),
    [12, 14],
  );
  state = reduce(state, { scope, type: "close", number: 14 });
  assert.equal(state.active, 12);
  state = reduce(state, { scope, type: "open", number: 15 });
  state = reduce(state, { scope, type: "open", number: 12 });
  state = reduce(state, { scope, type: "close", number: 12 });
  assert.equal(state.active, 15);
  state = reduce(state, { scope, type: "close", number: 15 });
  assert.equal(state.active, 0);
  assert.equal(
    state.initialized,
    true,
    "closing the last tab must suppress automatic reopening",
  );
});
test("repository changes clear tabs and reject late actions from the old repository", () => {
  let state = reduce(emptyPullRequestTabs(scope), {
    scope,
    type: "open",
    number: 12,
    section: "conversation",
  });
  state = reduce(state, { scope: "repo-b", type: "scope" });
  assert.deepEqual(state, emptyPullRequestTabs("repo-b"));
  state = reduce(state, { scope: "repo-b", type: "open", number: 12 });
  const expected = state;
  for (const action of [
    { scope, type: "open" as const, number: 99 },
    {
      scope,
      type: "section" as const,
      number: 12,
      section: "conversation" as const,
    },
    { scope, type: "close" as const, number: 12 },
  ])
    state = reduce(state, action);
  assert.equal(state, expected);
  assert.equal(state.items[0].section, "changes");
});
test("creation and notification opens can explicitly select a section without changing another tab", () => {
  let state = reduce(emptyPullRequestTabs(scope), {
    scope,
    type: "open",
    number: 12,
  });
  state = reduce(state, {
    scope,
    type: "open",
    number: 99,
    section: "conversation",
  });
  assert.equal(state.items[0].section, "changes");
  assert.equal(state.items[1].section, "conversation");
  state = reduce(state, { scope, type: "close", number: 12 });
  state = reduce(state, {
    scope,
    type: "section",
    number: 12,
    section: "checks",
  });
  assert.deepEqual(
    state.items.map((item) => item.number),
    [99],
  );
});
test("invalid PR numbers never create tabs and resetting allows a fresh default selection", () => {
  let state = emptyPullRequestTabs(scope);
  for (const number of [0, -1, NaN, 1.5, Infinity])
    state = reduce(state, { scope, type: "open", number });
  assert.deepEqual(state.items, []);
  state = reduce(state, { scope, type: "open", number: 12 });
  state = reduce(state, { scope, type: "reset" });
  assert.deepEqual(state, emptyPullRequestTabs(scope));
});
