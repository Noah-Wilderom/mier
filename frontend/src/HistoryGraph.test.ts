// @ts-nocheck -- Node executes TypeScript directly; tsc checks the topology module.
import test from "node:test";
import assert from "node:assert/strict";
import { buildHistoryGraph, commitRefs, historyEdgePath } from "./HistoryGraph.topology.ts";

const commit = (sha, parents = []) => ({ sha, parents });
const edges = (layout) => layout.edges.map((edge) => `${edge.childSha}->${edge.parentSha}`);
const lanes = (layout) => Object.fromEntries(layout.rows.map((row) => [row.commit.sha, row.lane]));

test("linear ancestry contains only actual parent relationships", () => {
  const layout = buildHistoryGraph([commit("c", ["b"]), commit("b", ["a"]), commit("a")]);
  assert.deepEqual(edges(layout), ["c->b", "b->a"]);
  assert.deepEqual(lanes(layout), { c: 0, b: 0, a: 0 });
  assert.equal(layout.laneCount, 1);
  assert.equal(historyEdgePath(layout.edges[0]), "M16 21V63");
});

test("a merge reserves distinct branches until the shared ancestor", () => {
  const layout = buildHistoryGraph([
    commit("merge", ["left", "right"]),
    commit("left", ["root"]),
    commit("right", ["root"]),
    commit("root"),
  ]);
  assert.deepEqual(edges(layout), ["merge->left", "merge->right", "left->root", "right->root"]);
  assert.deepEqual(lanes(layout), { merge: 0, left: 0, right: 1, root: 0 });
  assert.equal(layout.laneCount, 2);
  assert.match(historyEdgePath(layout.edges[1]), /^M16 21C16 31 34 31 34 42V105$/);
});

test("two independent heads share only their known parent", () => {
  const layout = buildHistoryGraph([commit("a", ["shared"]), commit("b", ["shared"]), commit("shared")]);
  assert.deepEqual(lanes(layout), { a: 0, b: 1, shared: 0 });
  assert.deepEqual(edges(layout), ["a->shared", "b->shared"]);
});

test("disconnected roots never receive a connector", () => {
  const layout = buildHistoryGraph([commit("a"), commit("b"), commit("c")]);
  assert.deepEqual(edges(layout), []);
  assert.deepEqual(lanes(layout), { a: 0, b: 0, c: 0 });
});

test("missing parents, unavailable metadata and actual roots remain distinct", () => {
  const layout = buildHistoryGraph([commit("boundary", ["outside"]), { sha: "unknown" }, commit("root")]);
  assert.deepEqual(edges(layout), []);
  assert.deepEqual(layout.rows[0].missingParents, ["outside"]);
  assert.equal(layout.rows[0].parentMetadataUnavailable, false);
  assert.equal(layout.rows[1].parentMetadataUnavailable, true);
  assert.equal(layout.rows[2].parentMetadataUnavailable, false);
  assert.deepEqual(layout.rows[2].missingParents, []);
});

test("a visible secondary parent diverges when the first parent is not loaded", () => {
  const layout = buildHistoryGraph([commit("merge", ["outside", "secondary"]), commit("secondary")]);
  assert.deepEqual(lanes(layout), { merge: 0, secondary: 1 });
  assert.deepEqual(edges(layout), ["merge->secondary"]);
  assert.deepEqual(layout.rows[0].missingParents, ["outside"]);
});

test("unordered input is sorted child before parent with stable unrelated ties", () => {
  const layout = buildHistoryGraph([commit("root"), commit("child", ["root"]), commit("unrelated")]);
  assert.deepEqual(layout.rows.map((row) => row.commit.sha), ["child", "root", "unrelated"]);
  assert.deepEqual(edges(layout), ["child->root"]);
});

test("full IDs are exact even with identical short prefixes", () => {
  const first = "abcdef0123456789";
  const second = "abcdef0fedcba987";
  const layout = buildHistoryGraph([commit("child", [first, "abcdef0"]), commit(first), commit(second)]);
  assert.deepEqual(edges(layout), [`child->${first}`]);
  assert.deepEqual(layout.rows[0].missingParents, ["abcdef0"]);
});

test("duplicate commits and parent IDs do not duplicate nodes or edges", () => {
  const layout = buildHistoryGraph([commit("child", ["root", "root"]), commit("root"), commit("root", ["unknown"])]);
  assert.equal(layout.rows.length, 2);
  assert.deepEqual(edges(layout), ["child->root"]);
});

test("cyclic and self-parent metadata stay inspectable without false edges", () => {
  const cycle = buildHistoryGraph([commit("a", ["b"]), commit("b", ["a"])]);
  assert.equal(cycle.hasInvalidTopology, true);
  assert.equal(cycle.rows.length, 2);
  assert.ok(cycle.rows.every((row) => row.invalidTopology));
  assert.deepEqual(edges(cycle), []);
  const self = buildHistoryGraph([commit("self", ["self"])]);
  assert.equal(self.hasInvalidTopology, true);
  assert.deepEqual(edges(self), []);
});

test("reserved lanes avoid unrelated nodes and layout does not mutate input", () => {
  const input = [commit("head", ["old"]), commit("separate"), commit("old")];
  const original = structuredClone(input);
  const first = buildHistoryGraph(input);
  assert.deepEqual(lanes(first), { head: 0, separate: 1, old: 0 });
  assert.deepEqual(buildHistoryGraph(input), first);
  assert.deepEqual(input, original);
});

test("reference labels preserve full identities without duplicate empty chips", () => {
  assert.deepEqual(commitRefs({ sha: "a", refs: ["main", "origin/main", "main", "", " ", "tag: v1.0"] }), ["main", "origin/main", "tag: v1.0"]);
});
