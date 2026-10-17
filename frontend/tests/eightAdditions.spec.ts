import { test as base, expect } from "@playwright/test";
import {
  desktopFixture,
  openFixture,
  root,
  repository,
  summary,
  detail,
  diff,
  headSHA,
  baseSHA,
  changedSHA,
} from "./desktopFixture";
import { catalogServers } from "./languageServerFixture";
import type { AIReview } from "../src/aiTypes";

const test = base.extend<{
  desktop: Awaited<ReturnType<typeof desktopFixture>>;
}>({
  desktop: async ({ page }, use) => {
    const fixture = await desktopFixture(page);
    await use(fixture);
    expect(fixture.unexpected).toEqual([]);
    expect(fixture.pageErrors).toEqual([]);
  },
});
const row = (page: any, number: number) =>
  page.locator(".pr-list-item").filter({ hasText: `Review ${number}` });

test("restart restores the repository, PR tabs, sections and private conversation drafts", async ({
  desktop,
  page,
}) => {
  desktop.prs = [summary(headSHA, 12), summary(headSHA, 13)];
  await openFixture(desktop);
  await row(page, 12).click();
  await page.getByRole("tab", { name: /^Conversation/ }).click();
  await page
    .locator(".conversation-composer textarea")
    .fill("Retain my private conversation draft");
  await row(page, 13).click();
  await page.getByRole("tab", { name: /^Conversation/ }).click();
  await page
    .locator(".conversation-composer textarea")
    .fill("A separate unfinished reply");
  await expect
    .poll(() =>
      [...desktop.savedStates.values()].some((value) =>
        value.includes("A separate unfinished reply"),
      ),
    )
    .toBe(true);
  await page.reload();
  await page.getByRole("region", { name: "Repository Git actions" }).waitFor();
  await desktop.emit();
  const tabs = page.getByRole("tablist", {
    name: "Open pull requests",
    exact: true,
  });
  await expect(tabs.getByRole("tab")).toHaveCount(2);
  await expect(
    tabs.getByRole("tab", { name: /^Pull request #13:/ }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".conversation-composer textarea")).toHaveValue(
    "A separate unfinished reply",
  );
  await tabs.getByRole("tab", { name: /^Pull request #12:/ }).click();
  await expect(page.locator(".conversation-composer textarea")).toHaveValue(
    "Retain my private conversation draft",
  );
  expect([...desktop.savedStates.values()].join("")).not.toContain(
    '"submitting":true',
  );
});

test("review checkpoints compare exact old and new heads without changing PR comment anchors", async ({
  desktop,
  page,
}) => {
  desktop.handlers.set("main.RepositoryService.CompareReviewHeads", () => [
    diff("src/feature.ts", "const value = 3"),
  ]);
  await openFixture(desktop);
  await row(page, 12).click();
  await page.getByRole("button", { name: "Save review checkpoint" }).click();
  await expect
    .poll(() =>
      [...desktop.savedStates.keys()].some((key) =>
        key.startsWith("checkpoint:"),
      ),
    )
    .toBe(true);
  desktop.prs = [summary(changedSHA)];
  desktop.currentDetail = detail(changedSHA);
  await desktop.emit({ pullRequest: desktop.currentDetail });
  await page
    .getByRole("button", { name: "Refresh changes", exact: true })
    .click();
  await expect(
    page.locator('.review-code-text[data-code-side="new"]').first(),
  ).toContainText("const value = 3");
  await page.getByLabel("Review comparison").selectOption("incremental");
  await expect(page.locator(".comparison-code")).toContainText(
    "const value = 3",
  );
  expect(
    desktop.calls.find((call) => call.name.endsWith(".CompareReviewHeads"))
      ?.args[0],
  ).toMatchObject({ root, previousHead: headSHA, head: changedSHA });
  await expect(
    page.getByRole("button", { name: /^Comment on changed/ }),
  ).toHaveCount(0);
  await page.getByLabel("Review comparison").selectOption("full");
  await expect(
    page.getByRole("button", {
      name: "Comment on changed line 1 in src/feature.ts",
    }),
  ).toBeVisible();
});

test("outline and repository symbols navigate through the revision-bound reader", async ({
  desktop,
  page,
}) => {
  const servers = catalogServers().map((server) =>
    server.language === "typescript"
      ? { ...server, available: true, error: "" }
      : server,
  );
  desktop.handlers.set("main.LSPService.Servers", () => servers);
  const target = {
    id: "symbol-1",
    uri: "mier-lsp://definition/symbol-1",
    path: "src/feature.ts",
    range: {
      start: { line: 0, character: 6 },
      end: { line: 0, character: 11 },
    },
    revision: headSHA,
    language: "typescript",
    external: false,
    name: "value",
    kind: 13,
    depth: 0,
  };
  desktop.handlers.set("main.LSPService.DocumentSymbols", () => [target]);
  desktop.handlers.set("main.LSPService.WorkspaceSymbols", () => [target]);
  desktop.handlers.set("main.LSPService.References", () => [target]);
  desktop.handlers.set("main.LSPService.ReadDefinition", () => ({
    uri: target.uri,
    text: "const value = 2",
    path: target.path,
    language: target.language,
    revision: headSHA,
    range: target.range,
    external: false,
  }));
  await openFixture(desktop);
  await row(page, 12).click();
  await page.getByRole("button", { name: "Outline", exact: true }).click();
  const outline = page.getByRole("dialog", { name: "Document outline" });
  await expect(outline).toContainText("value");
  await outline
    .getByRole("button", { name: /value · src\/feature.ts:1/ })
    .click();
  await expect(page.locator(".code-definition-reader")).toContainText(
    "const value = 2",
  );
  expect(
    desktop.calls.find((call) => call.name.endsWith(".DocumentSymbols"))
      ?.args[0].revision,
  ).toBe(headSHA);
  await page
    .getByRole("button", { name: "Back to review", exact: true })
    .click();
  await page.getByLabel("Search repository symbols").fill("value");
  await page.getByRole("button", { name: "Find", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "Repository symbols" }),
  ).toContainText("value");
});

test("large diffs mount nearby chunks first and reveal distant rows on scroll", async ({
  desktop,
  page,
}) => {
  desktop.currentDetail.files![0].patch =
    "@@ -1,2000 +1,2000 @@\n" +
    Array.from({ length: 2000 }, (_, i) => ` const value${i} = ${i}`).join(
      "\n",
    ) +
    "\n";
  await openFixture(desktop);
  await row(page, 12).click();
  await expect(page.locator(".review-diff-chunk")).toHaveCount(34);
  expect(await page.locator(".review-split-row").count()).toBeLessThan(1000);
  await page
    .locator(".review-diff-scroll")
    .evaluate((element) => (element.scrollTop = element.scrollHeight));
  await expect(
    page.locator('[data-code-side="new"][data-code-line="2000"]'),
  ).toContainText("value1999");
});

test("cross-repository inbox saves project groups and opens the chosen repository PR", async ({
  desktop,
  page,
}) => {
  const other = "/fixture/other-project",
    snapshot = repository(other);
  snapshot.remotes = snapshot.remotes!.map((remote) => ({
    ...remote,
    repository: "other",
    fetchUrl: "https://github.com/fork/other.git",
  }));
  desktop.repositories.set(other, snapshot);
  desktop.recent = [root, other];
  let switchingRepository = false;
  desktop.handlers.set("main.RepositoryService.Open", (args) => {
    switchingRepository = args[0] === other;
    return desktop.repositories.get(args[0]);
  });
  desktop.handlers.set("main.CredentialService.AccountStatus", async (args) => {
    // Local Git can open before a slower credential lookup has finished.
    if (switchingRepository)
      await new Promise((resolve) => setTimeout(resolve, 500));
    return {
      host: args[0],
      login: "reviewer",
      authenticated: true,
      source: "gh",
      name: "Reviewer",
      avatarUrl: "",
      url: "https://github.com/reviewer",
      error: "",
    };
  });
  desktop.handlers.set("main.GitHubService.ListPullRequestsPage", (args) => ({
    items: [
      args[2] === "other" && args[3].view === "review-requested"
        ? { ...summary(headSHA, 99), title: "Other project review" }
        : summary(),
    ],
    nextCursor: "",
    hasNextPage: false,
    totalCount: 1,
    openCount: 1,
  }));
  await openFixture(desktop);
  await page
    .locator(".sidebar")
    .getByRole("button", { name: "All repositories", exact: true })
    .click();
  const inbox = page.getByRole("region", { name: "All repositories inbox" });
  await expect(
    inbox.getByRole("button", { name: /Other project review/ }),
  ).toBeVisible();
  await inbox
    .getByText("Saved filters and project groups", { exact: true })
    .click();
  await inbox
    .getByRole("checkbox", { name: "other-project", exact: true })
    .check();
  await inbox.getByLabel("Filter or group name").fill("Delivery");
  await inbox
    .getByRole("button", { name: "Save project group", exact: true })
    .click();
  await expect(inbox.getByLabel("Inbox project group")).toHaveValue("Delivery");
  await expect(
    inbox.getByRole("heading", { name: "fork/app github.com" }),
  ).toHaveCount(0);
  await inbox.getByRole("button", { name: /Other project review/ }).click();
  await expect(
    page.getByRole("tab", { name: /^Pull request #99:/ }),
  ).toBeVisible();
  await expect
    .poll(() =>
      [...desktop.savedStates].some(
        ([key, value]) =>
          key.startsWith("tabs:") &&
          key.includes(other) &&
          key.includes("reviewer") &&
          JSON.parse(value).state.active === 99,
      ),
    )
    .toBe(true);
  expect(
    desktop.calls.some(
      (call) => call.name.endsWith(".Open") && call.args[0] === other,
    ),
  ).toBe(true);
});

test("optional conflict forecast refreshes on target changes and previews each rebase step", async ({
  desktop,
  page,
}) => {
  let finishRebase: (() => void) | undefined;
  desktop.handlers.set(
    "main.RepositoryService.ForecastConflict",
    async (args) => {
      if (args[1].rebase)
        await new Promise<void>((resolve) => {
          finishRebase = resolve;
        });
      return {
        head: headSHA,
        target: args[1].expectedTarget,
        base: baseSHA,
        clean: false,
        conflicts: ["src/feature.ts"],
        overlap: ["src/feature.ts"],
        rebase: args[1].rebase
          ? [{ commit: headSHA, clean: false, conflicts: ["src/feature.ts"] }]
          : [],
        rebaseComplete: false,
        note: "",
      };
    },
  );
  await openFixture(desktop);
  expect(
    desktop.calls.some((call) => call.name.endsWith(".ForecastConflict")),
  ).toBe(false);
  await page
    .getByRole("button", { name: "Conflict forecast", exact: true })
    .click();
  const panel = page.getByRole("region", {
    name: "Proactive conflict detection",
  });
  await panel.getByLabel("Conflict forecast target").selectOption("main");
  await panel.getByRole("checkbox", { name: "Check in background" }).check();
  await expect(panel).toContainText(
    "A merge is expected to stop for conflicts.",
  );
  await panel.getByRole("button", { name: "Preview rebase" }).click();
  await expect.poll(() => typeof finishRebase).toBe("function");
  await expect(panel).not.toContainText(
    "Rebase preview stopped at the first conflict",
  );
  await expect(panel).not.toContainText("Rebase preview completed");
  finishRebase!();
  await expect(panel).toContainText(
    "Rebase preview stopped at the first conflict",
  );
  expect(
    desktop.calls.find(
      (call) => call.name.endsWith(".ForecastConflict") && call.args[1].rebase,
    )?.args[1],
  ).toMatchObject({
    target: "main",
    expectedHead: headSHA,
    expectedTarget: baseSHA,
  });
});

test("CI failure annotations appear inline and open unchanged source at the exact checked commit", async ({
  desktop,
  page,
}) => {
  desktop.currentDetail.checks = [
    {
      id: 321,
      name: "Compiler",
      status: "completed",
      conclusion: "failure",
      startedAt: "",
      completedAt: "",
      detailsUrl: "",
      app: { slug: "other-ci" },
      output: { title: "", summary: "", text: "" },
    },
  ];
  desktop.handlers.set("main.GitHubService.GetCheckAnnotations", () => ({
    headSha: headSHA,
    hasMore: false,
    items: [
      {
        path: "src/feature.ts",
        startLine: 1,
        endLine: 1,
        startColumn: 1,
        endColumn: 5,
        annotationLevel: "failure",
        message: "Compiler error at the reviewed line",
        title: "Compilation",
        rawDetails: "",
      },
      {
        path: "src/unchanged.ts",
        startLine: 4,
        endLine: 4,
        startColumn: 1,
        endColumn: 1,
        annotationLevel: "failure",
        message: "A failure outside the PR patch",
        title: "Compilation",
        rawDetails: "",
      },
    ],
  }));
  desktop.handlers.set("main.GitHubService.ReadCheckAnnotationSource", () => ({
    path: "src/unchanged.ts",
    revision: headSHA,
    text: 'const a = 1\nconst b = 2\nconst c = 3\nthrow Error("failure")\n',
    line: 4,
  }));
  await openFixture(desktop);
  await row(page, 12).click();
  await expect(page.locator(".ci-inline")).toContainText(
    "Compiler error at the reviewed line",
  );
  await page.getByRole("tab", { name: /^Checks/ }).click();
  await page.getByRole("button", { name: /Compiler Optional/ }).click();
  await page
    .getByRole("button", { name: "src/unchanged.ts:4", exact: true })
    .click();
  const source = page.getByRole("dialog", {
    name: "CI source · src/unchanged.ts:4",
  });
  await expect(source.getByLabel("CI annotation source")).toContainText(
    "throw Error",
  );
  await expect(source.locator(".cm-content")).toHaveAttribute(
    "contenteditable",
    "false",
  );
  expect(
    desktop.calls
      .find((call) => call.name.endsWith(".ReadCheckAnnotationSource"))
      ?.args.slice(3),
  ).toEqual([321, headSHA, "src/unchanged.ts", 4, 1]);
});

test("AI review history restores and a follow-up explicitly references the earlier review", async ({
  desktop,
  page,
}) => {
  const previous: AIReview = {
    id: "older-review",
    key: "fixture",
    provider: "codex",
    model: "fixture-model",
    intensity: "medium",
    instructions: "Check the retry path",
    headSha: headSHA,
    state: "completed",
    progress: "",
    summary: "A retry issue was identified",
    error: "",
    findings: [],
    version: 5,
    createdAt: "2026-10-16T10:00:00Z",
    finishedAt: "2026-10-16T10:02:00Z",
    warnings: [],
  };
  let current = previous;
  desktop.handlers.set("main.AIService.ReviewHistory", () => [previous]);
  desktop.handlers.set("main.AIService.GetReview", () => current);
  desktop.handlers.set("main.AIService.StartReview", (args) => {
    expect(args[0].previousReviewId).toBe(previous.id);
    current = {
      ...previous,
      id: "follow-up",
      instructions: args[0].instructions,
      summary: "The earlier finding still applies",
    };
    return current;
  });
  await openFixture(desktop);
  await row(page, 12).click();
  await page
    .getByRole("button", { name: "Review with AI", exact: true })
    .click();
  const pane = page.getByRole("complementary", {
    name: "AI reviewer",
    exact: true,
  });
  await expect(pane).toContainText("A retry issue was identified");
  await pane
    .getByLabel("Review focus (optional)")
    .fill("Does the earlier retry finding still apply?");
  await pane
    .getByRole("button", { name: "Send follow-up", exact: true })
    .click();
  await expect(pane).toContainText("The earlier finding still applies");
  await expect(pane).toContainText("Check the retry path");
});
