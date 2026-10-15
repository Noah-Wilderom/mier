import { test as base, expect } from "@playwright/test";
import {
  desktopFixture,
  openFixture,
  summary,
  detail,
  diff,
  headSHA,
  repository,
  root,
} from "./desktopFixture";

const test = base.extend<{
  desktop: Awaited<ReturnType<typeof desktopFixture>>;
}>({
  desktop: async ({ page }, use) => {
    const fixture = await desktopFixture(page);
    fixture.prs = [12, 13, 14].map((number) => summary(headSHA, number));
    await use(fixture);
    expect(fixture.unexpected).toEqual([]);
    expect(fixture.pageErrors).toEqual([]);
  },
});
const opened = (page: Awaited<ReturnType<typeof desktopFixture>>["page"]) =>
  page.getByRole("tablist", { name: "Open pull requests", exact: true });
const prTab = (
  page: Awaited<ReturnType<typeof desktopFixture>>["page"],
  number: number,
) =>
  opened(page).getByRole("tab", {
    name: new RegExp(`^Pull request #${number}:`),
  });
const row = (
  page: Awaited<ReturnType<typeof desktopFixture>>["page"],
  number: number,
) => page.locator(".pr-list-item").filter({ hasText: `Review ${number}` });

test("opened PRs have one closable tab each and remember separate conversation drafts and sections", async ({
  desktop,
  page,
}) => {
  await openFixture(desktop);
  await row(page, 12).click();
  await page.getByRole("tab", { name: /^Conversation/ }).click();
  await page
    .locator(".conversation-composer textarea")
    .fill("My unfinished comment for twelve");
  await row(page, 13).click();
  await expect(opened(page).getByRole("tab")).toHaveCount(2);
  await expect(page.getByRole("tab", { name: /^Changes/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page.getByRole("tab", { name: /^Conversation/ }).click();
  await expect(page.locator(".conversation-composer textarea")).toHaveValue("");
  await page
    .locator(".conversation-composer textarea")
    .fill("A different comment for thirteen");
  await prTab(page, 12).click();
  await expect(
    page.getByRole("tab", { name: /^Conversation/ }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".conversation-composer textarea")).toHaveValue(
    "My unfinished comment for twelve",
  );
  await row(page, 12).click();
  await expect(opened(page).getByRole("tab")).toHaveCount(2);
  await page
    .locator(".sidebar")
    .getByRole("button", { name: "Review requested", exact: true })
    .click();
  await expect(opened(page).getByRole("tab")).toHaveCount(2);
  await prTab(page, 13).click();
  await expect(page.locator(".conversation-composer textarea")).toHaveValue(
    "A different comment for thirteen",
  );
  await page
    .getByRole("button", { name: "Close pull request #12", exact: true })
    .click();
  await expect(prTab(page, 13)).toHaveAttribute("aria-selected", "true");
  await row(page, 12).click();
  await page.getByRole("tab", { name: /^Conversation/ }).click();
  await expect(page.locator(".conversation-composer textarea")).toHaveValue(
    "My unfinished comment for twelve",
  );
});

test("switching PRs retains the selected file, diff layout and unfinished inline comment without another mounted editor", async ({
  desktop,
  page,
}) => {
  const second = diff("src/second.ts");
  desktop.currentDetail.files!.push({
    path: second.path,
    previousPath: "",
    status: "modified",
    additions: 1,
    deletions: 1,
    patch: second.rawPatch,
  });
  await openFixture(desktop);
  await row(page, 12).click();
  await page
    .getByRole("button", { name: "Modified second.ts", exact: true })
    .click();
  await page.getByRole("button", { name: "Unified", exact: true }).click();
  await page
    .getByRole("checkbox", { name: "Wrap lines", exact: true })
    .uncheck();
  await page
    .locator(".review-unified-row.diff-add")
    .getByRole("button", {
      name: "Comment on line 1 in src/second.ts",
      exact: true,
    })
    .click();
  await page
    .locator("#new-review-comment")
    .fill("Keep this unfinished inline comment on PR twelve");
  await row(page, 13).click();
  await expect(page.locator(".review-workspace")).toHaveCount(1);
  await expect(page.locator("#new-review-comment")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Split", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await prTab(page, 12).click();
  await expect(page.locator(".review-file-row.is-active")).toContainText(
    "second.ts",
  );
  await expect(
    page.getByRole("button", { name: "Unified", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(
    page.getByRole("checkbox", { name: "Wrap lines", exact: true }),
  ).not.toBeChecked();
  await expect(page.locator("#new-review-comment")).toHaveValue(
    "Keep this unfinished inline comment on PR twelve",
  );
  await page
    .locator(".review-inline-composer")
    .getByRole("button", { name: "Add to review", exact: true })
    .click();
  await expect(
    page.getByRole("region", {
      name: "Pending comment on line 1",
      exact: true,
    }),
  ).toContainText("PR twelve");
  await prTab(page, 13).click();
  await expect(
    page.getByRole("region", {
      name: "Pending comment on line 1",
      exact: true,
    }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Close pull request #12", exact: true })
    .click();
  await row(page, 12).click();
  await expect(
    page.getByRole("region", {
      name: "Pending comment on line 1",
      exact: true,
    }),
  ).toContainText("PR twelve");
});

test("keyboard navigation and closing tabs choose the neighboring PR and the final tab stays closed", async ({
  desktop,
  page,
}) => {
  await openFixture(desktop);
  for (const number of [12, 13, 14]) await row(page, number).click();
  await page
    .getByRole("button", { name: "Close pull request #13", exact: true })
    .click();
  await expect(prTab(page, 14)).toHaveAttribute("aria-selected", "true");
  await prTab(page, 12).focus();
  await page.keyboard.press("ArrowRight");
  await expect(prTab(page, 14)).toBeFocused();
  await page.keyboard.press("Home");
  await expect(prTab(page, 12)).toBeFocused();
  await page.keyboard.press("End");
  await expect(prTab(page, 14)).toBeFocused();
  await page.keyboard.press("Delete");
  await expect(prTab(page, 12)).toBeFocused();
  await page
    .getByRole("button", { name: "Expand code review", exact: true })
    .click();
  await expect(opened(page)).toBeVisible();
  await page
    .getByRole("button", { name: "Close pull request #12", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "No pull requests open", exact: true }),
  ).toBeVisible();
  await expect(opened(page)).toHaveCount(0);
  await expect(page.locator(".app-shell")).not.toHaveClass(/review-focused/);
  await desktop.emit();
  await page.getByRole("button", { name: "Merged", exact: true }).click();
  await page
    .getByRole("region", { name: "Pull request inbox", exact: true })
    .getByRole("button", { name: /^Open/ })
    .click();
  await expect(opened(page)).toHaveCount(0);
  expect(
    desktop.calls.some(
      (call) => call.name.endsWith(".GetPullRequest") && call.args[3] === 0,
    ),
  ).toBe(false);
  await row(page, 14).click();
  await expect(opened(page).getByRole("tab")).toHaveCount(1);
  await expect(prTab(page, 14)).toHaveAttribute("aria-selected", "true");
});

test("a loading background PR remains in its tab across inbox filters and late details do not switch tabs", async ({
  desktop,
  page,
}) => {
  let release!: () => void;
  desktop.handlers.set("main.GitHubService.GetPullRequest", async (args) => {
    if (args[3] === 13)
      await new Promise<void>((resolve) => {
        release = resolve;
      });
    return { ...detail(), pullRequest: summary(headSHA, args[3]) };
  });
  await openFixture(desktop);
  await row(page, 12).click();
  await expect(page.locator(".review-code-text").first()).toBeVisible();
  await row(page, 13).click();
  await expect.poll(() => typeof release).toBe("function");
  await prTab(page, 12).click();
  await page
    .getByLabel("Search pull requests", { exact: true })
    .fill("Review 14");
  await expect(page.locator(".pr-list-item")).toHaveCount(1);
  await expect(prTab(page, 13)).toContainText("Review 13");
  release();
  await expect(prTab(page, 12)).toHaveAttribute("aria-selected", "true");
  await prTab(page, 13).click();
  await expect(
    page.getByRole("region", { name: "Pull request 13", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".review-code-text").first()).toBeVisible();
  await page
    .locator(".sidebar")
    .getByRole("button", { name: "My pull requests", exact: true })
    .click();
  await expect(page.locator(".pr-list-item")).toHaveCount(0);
  await expect(prTab(page, 13)).toHaveAttribute("aria-selected", "true");
  await expect(
    page.getByRole("region", { name: "Pull request 13", exact: true }),
  ).toBeVisible();
});

test("editor scroll and an unfinished review summary return with their PR tab", async ({
  desktop,
  page,
}) => {
  const lines = Array.from(
    { length: 180 },
    (_, index) => ` const line${index + 1} = ${index + 1}`,
  );
  desktop.currentDetail.files![0].patch = `@@ -1,180 +1,180 @@\n${lines.join("\n")}\n`;
  await openFixture(desktop);
  await row(page, 12).click();
  const viewport = page.locator(".review-diff-scroll");
  await viewport.evaluate((element) => {
    element.scrollTop = 600;
  });
  await expect
    .poll(() => viewport.evaluate((element) => element.scrollTop))
    .toBe(600);
  await page
    .getByRole("button", { name: "Finish review", exact: true })
    .click();
  await page.locator("#review-summary").fill("Unfinished summary for twelve");
  await row(page, 13).click();
  await expect(page.locator("#review-summary")).toHaveCount(0);
  await expect
    .poll(() => viewport.evaluate((element) => element.scrollTop))
    .toBe(0);
  await prTab(page, 12).click();
  await expect(page.locator("#review-summary")).toHaveValue(
    "Unfinished summary for twelve",
  );
  await expect
    .poll(() => viewport.evaluate((element) => element.scrollTop))
    .toBe(600);
});

test("a comment posted from one PR completes in its own tab while another draft stays intact", async ({
  desktop,
  page,
}) => {
  let release!: () => void;
  let posted = false;
  const saved = {
    id: 101,
    nodeId: "comment-101",
    body: "Post this from twelve",
    createdAt: "2026-10-15T12:00:00Z",
    user: { login: "reviewer", name: "Reviewer", avatarUrl: "", url: "" },
  };
  desktop.handlers.set("main.GitHubService.AddIssueComment", async (args) => {
    expect(args.slice(0, 4)).toEqual(["github.com", "fork", "app", 12]);
    expect(args[4]).toBe(saved.body);
    await new Promise<void>((resolve) => {
      release = resolve;
    });
    posted = true;
    return saved;
  });
  desktop.handlers.set("main.GitHubService.GetPullRequest", (args) => ({
    ...detail(),
    pullRequest: summary(headSHA, args[3]),
    comments: args[3] === 12 && posted ? [saved] : [],
  }));
  await openFixture(desktop);
  await row(page, 12).click();
  await page.getByRole("tab", { name: /^Conversation/ }).click();
  await page.locator(".conversation-composer textarea").fill(saved.body);
  await page
    .locator(".conversation-composer")
    .getByRole("button", { name: "Comment", exact: true })
    .click();
  await expect.poll(() => typeof release).toBe("function");
  await row(page, 13).click();
  await page.getByRole("tab", { name: /^Conversation/ }).click();
  await page
    .locator(".conversation-composer textarea")
    .fill("Still typing on thirteen");
  release();
  await expect(prTab(page, 13)).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".conversation-composer textarea")).toHaveValue(
    "Still typing on thirteen",
  );
  await expect(page.locator(".conversation-pane")).not.toContainText(
    saved.body,
  );
  await prTab(page, 12).click();
  await expect(page.locator(".conversation-composer textarea")).toHaveValue("");
  await expect(page.locator(".conversation-pane")).toContainText(saved.body);
});

test("repository changes isolate tabs and drafts even when both repositories have PR twelve", async ({
  desktop,
  page,
}) => {
  await openFixture(desktop);
  await row(page, 12).click();
  await page.getByRole("tab", { name: /^Conversation/ }).click();
  await page
    .locator(".conversation-composer textarea")
    .fill("Private draft belonging to the first repository");
  await row(page, 13).click();
  const otherRoot = `${root}-other`;
  const other = repository(otherRoot);
  other.remotes = [
    {
      name: "origin",
      fetchUrl: "git@github.com:another-owner/another-app.git",
      pushUrl: "git@github.com:another-owner/another-app.git",
      host: "github.com",
      owner: "another-owner",
      repository: "another-app",
      provider: "github",
    },
  ];
  desktop.repositories.set(otherRoot, other);
  desktop.selected = otherRoot;
  desktop.prs = [summary()];
  await page.keyboard.press("Control+o");
  await expect(opened(page).getByRole("tab")).toHaveCount(1);
  await expect(prTab(page, 12)).toHaveAttribute("aria-selected", "true");
  await page.getByRole("tab", { name: /^Conversation/ }).click();
  await expect(page.locator(".conversation-composer textarea")).toHaveValue("");
  expect(
    desktop.calls
      .filter((call) => call.name.endsWith(".GetPullRequest"))
      .at(-1)
      ?.args.slice(0, 3),
  ).toEqual(["github.com", "another-owner", "another-app"]);
});

test("many PR tabs scroll within their strip at smaller widths and keep the active close control visible", async ({
  desktop,
  page,
}) => {
  desktop.prs = Array.from({ length: 20 }, (_, index) =>
    summary(headSHA, index + 10),
  );
  await openFixture(desktop);
  for (const number of [10, 11, 12, 13, 14, 15, 16, 17])
    await row(page, number).click();
  await page.setViewportSize({ width: 980, height: 760 });
  await prTab(page, 10).click();
  await prTab(page, 17).click();
  const strip = (await opened(page).boundingBox())!,
    close = (await page
      .getByRole("button", { name: "Close pull request #17", exact: true })
      .boundingBox())!;
  expect(close.x + close.width).toBeLessThanOrEqual(strip.x + strip.width + 1);
  expect(
    await opened(page).evaluate(
      (element) => element.scrollWidth > element.clientWidth,
    ),
  ).toBe(true);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await prTab(page, 16).click({ button: "middle" });
  await expect(prTab(page, 16)).toHaveCount(0);
  await expect(prTab(page, 17)).toHaveAttribute("aria-selected", "true");
});
