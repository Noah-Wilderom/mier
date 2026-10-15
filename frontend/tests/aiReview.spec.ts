import { test as base, expect } from "@playwright/test";
import {
  desktopFixture,
  openFixture,
  headSHA,
  baseSHA,
  changedSHA,
  detail,
  summary,
} from "./desktopFixture";
import type { AIConnection, AIReview } from "../src/aiTypes";

const finding = {
  id: "finding-a",
  path: "src/feature.ts",
  line: 1,
  side: "new" as const,
  body: "This change repeats a successful write. Return after success.",
  severity: "bug" as const,
};
function review(input: Partial<AIReview> = {}): AIReview {
  return {
    id: "review-a",
    key: "fixture",
    provider: "codex",
    model: "fixture-model",
    headSha: headSHA,
    state: "running",
    progress: "Inspecting pull request changes",
    summary: "",
    error: "",
    findings: [],
    version: 1,
    createdAt: "2026-10-14T12:00:00Z",
    finishedAt: "",
    warnings: [],
    ...input,
  };
}
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
async function openReview(desktop: Awaited<ReturnType<typeof desktopFixture>>) {
  await openFixture(desktop);
  await desktop.page.locator(".pr-list-item").first().click();
  await expect(desktop.page.locator(".review-workspace")).toBeVisible();
  await desktop.page
    .getByRole("button", { name: "Review with AI", exact: true })
    .click();
  await expect(
    desktop.page.getByRole("button", { name: "Start AI review", exact: true }),
  ).toBeEnabled();
}

test("model logos and all three intensities reach native reviews through the chat composer", async ({
  desktop,
  page,
}) => {
  let current: AIReview;
  const requests: { intensity: string; instructions: string }[] = [];
  desktop.handlers.set("main.AIService.GetProviderStatus", (args) => ({
    provider: args[0],
    installed: true,
    authenticated: true,
    account: "reviewer",
    plan: "",
    error: "",
    models: [
      { id: "gpt-review", name: "GPT review model", default: true },
      { id: "claude-review", name: "Claude review model", default: false },
      { id: "gemini-review", name: "Gemini review model", default: false },
    ],
  }));
  desktop.handlers.set("main.AIService.StartReview", (args) => {
    requests.push(args[0]);
    current = review({
      id: `review-${requests.length}`,
      provider: "copilot",
      model: args[0].model,
      intensity: args[0].intensity,
      instructions: args[0].instructions,
      state: "completed",
      summary: `Review ${requests.length} completed.`,
    });
    return current;
  });
  desktop.handlers.set("main.AIService.GetReview", () => current);
  desktop.handlers.set("main.AIService.SavePreferences", () => null);
  await openReview(desktop);
  await page.getByLabel("AI review provider").selectOption("copilot");
  await page.getByRole("combobox", { name: "AI review model" }).click();
  for (const [label, brand] of [
    ["GPT review model", "openai"],
    ["Claude review model", "claude"],
    ["Gemini review model", "gemini"],
  ]) {
    await expect(
      page.getByRole("option", { name: new RegExp(label) }).locator("img"),
    ).toHaveAttribute("src", `/ai/${brand}.svg`);
  }
  await page
    .getByRole("option", { name: "Claude review model", exact: true })
    .click();
  const draft = page.getByLabel("Review focus (optional)");
  await draft.fill("Keep this focus while the pane is hidden");
  await page
    .getByRole("button", { name: "Hide AI review", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Review with AI", exact: true })
    .click();
  await expect(draft).toHaveValue("Keep this focus while the pane is hidden");
  for (const [index, [label, intensity]] of [
    ["Quick", "low"],
    ["Balanced", "medium"],
    ["Thorough", "high"],
  ].entries()) {
    if (index) await page.locator(".ai-chat-settings > summary").click();
    await page.getByRole("radio", { name: label, exact: true }).check();
    await draft.fill(`Focus ${index}`);
    await draft.press("Shift+Enter");
    await draft.pressSequentially("Check failure handling");
    await draft.press("Enter");
    await expect.poll(() => requests.length).toBe(index + 1);
    expect(requests[index]).toMatchObject({
      intensity,
      instructions: `Focus ${index}\nCheck failure handling`,
    });
    await expect(page.locator(".ai-chat-user")).toContainText(`Focus ${index}`);
    await expect(page.locator(".ai-chat-assistant")).toContainText(
      `Review ${index + 1} completed.`,
    );
    await expect(draft).toHaveValue("");
  }
});

test("findings require explicit acceptance, support editing and preserve human drafts without GitHub writes", async ({
  desktop,
  page,
}) => {
  let current = review(),
    release!: () => void;
  desktop.handlers.set("main.AIService.GetReview", () => current);
  desktop.handlers.set("main.AIService.SavePreferences", () => null);
  desktop.handlers.set("main.AIService.StartReview", async (args) => {
    expect(args[0]).toEqual({
      host: "github.com",
      owner: "fork",
      repository: "app",
      pullRequest: 12,
      headSha: headSHA,
      baseSha: baseSHA,
      provider: "codex",
      model: "fixture-model",
      instructions: "Check retry handling",
      intensity: "high",
    });
    await new Promise<void>((resolve) => {
      release = resolve;
    });
    return current;
  });
  await openReview(desktop);
  await page.getByRole("combobox", { name: "AI review model" }).click();
  await page.getByRole("option", { name: /Fixture model/ }).click();
  await page.getByRole("radio", { name: "Thorough", exact: true }).check();
  await page.getByLabel("Review focus (optional)").fill("Check retry handling");
  await page
    .getByRole("button", { name: "Start AI review", exact: true })
    .evaluate((button) => {
      (button as HTMLButtonElement).click();
      (button as HTMLButtonElement).click();
    });
  await expect
    .poll(
      () =>
        desktop.calls.filter((call) => call.name.endsWith(".StartReview"))
          .length,
    )
    .toBe(1);
  release();
  current = review({ findings: [finding] });
  const pending = page.getByRole("region", {
    name: "Pending comment on line 1",
    exact: true,
  });
  const proposal = page.getByRole("article", {
    name: "AI finding at src/feature.ts:1",
  });
  await expect(proposal).toContainText("Return after success");
  await expect(pending).toHaveCount(0);
  await expect(
    proposal.getByRole("button", { name: "Add to review", exact: true }),
  ).toHaveCount(0);
  await proposal
    .getByRole("button", { name: "View code", exact: true })
    .click();
  const target = page.locator(".review-diff-cell.is-review-target");
  await expect(target).toHaveCount(1);
  expect(
    await target.evaluate((element) => {
      const probe = document.createElement("div");
      probe.style.background = "var(--blue-soft)";
      element.append(probe);
      const matches =
        getComputedStyle(element).backgroundColor ===
        getComputedStyle(probe).backgroundColor;
      probe.remove();
      return matches;
    }),
  ).toBe(true);
  current = review({
    state: "completed",
    findings: [finding],
    summary: "One actionable concern; tests were not run.",
  });
  await expect(
    proposal.getByRole("button", { name: "Edit comment", exact: true }),
  ).toBeEnabled();
  await expect(pending).toHaveCount(0);
  await page
    .getByRole("button", {
      name: "Comment on changed line 1 in src/feature.ts",
      exact: true,
    })
    .click();
  await page
    .locator("#new-review-comment")
    .fill("Please keep my human question as well.");
  await page
    .locator(".review-inline-composer")
    .getByRole("button", { name: "Add to review", exact: true })
    .click();
  await expect(pending).toHaveCount(1);
  await proposal
    .getByRole("button", { name: "Edit comment", exact: true })
    .click();
  await proposal
    .getByLabel("Review comment", { exact: true })
    .fill(
      "Please return after the successful write to avoid duplicate writes.",
    );
  await proposal
    .getByRole("button", { name: "Add edited comment", exact: true })
    .evaluate((button) => {
      (button as HTMLButtonElement).click();
      (button as HTMLButtonElement).click();
    });
  await expect(pending).toHaveCount(2);
  const aiPending = pending.filter({ hasText: "AI suggestion · bug" });
  await expect(aiPending).toContainText(
    "Please return after the successful write",
  );
  await expect(aiPending).not.toContainText("This change repeats");
  await aiPending
    .getByRole("button", {
      name: "Delete pending comment on line 1",
      exact: true,
    })
    .click();
  await expect(pending).toHaveCount(1);
  await page
    .getByRole("button", { name: "Hide AI review", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Review with AI", exact: true })
    .click();
  await expect(pending).toHaveCount(1);
  await proposal
    .getByRole("button", { name: "Add to review", exact: true })
    .click();
  await expect(pending).toHaveCount(2);
  await expect(proposal).toContainText("Added to your pending review");
  expect(
    desktop.calls.some((call) =>
      /SubmitReview|CreateReviewComment/.test(call.name),
    ),
  ).toBe(false);
});

test("findings support explicit batch selection, dismissal and restoration across navigation", async ({
  desktop,
  page,
}) => {
  const second = {
    ...finding,
    id: "finding-b",
    severity: "warning" as const,
    body: "A second concern requiring manual approval.",
  };
  let current = review({
    state: "completed",
    findings: [finding, second],
    summary: "Two proposed findings.",
  });
  desktop.handlers.set("main.AIService.StartReview", () => current);
  desktop.handlers.set("main.AIService.GetReview", () => current);
  desktop.handlers.set("main.AIService.SavePreferences", () => null);
  await openReview(desktop);
  await page
    .getByRole("button", { name: "Start AI review", exact: true })
    .click();
  const findings = page.getByRole("region", {
    name: "AI findings",
    exact: true,
  });
  const pending = page.getByRole("region", {
    name: "Pending comment on line 1",
    exact: true,
  });
  await expect(
    findings.getByLabel("Select all findings", { exact: true }),
  ).not.toBeChecked();
  await expect(
    findings.getByRole("button", { name: "Add selected (0)", exact: true }),
  ).toBeDisabled();
  await expect(pending).toHaveCount(0);
  await findings
    .getByRole("button", { name: "Dismiss", exact: true })
    .first()
    .click();
  await expect(findings.locator("article")).toHaveCount(1);
  await findings.getByLabel("Select all findings", { exact: true }).check();
  await findings
    .getByRole("button", { name: "Add selected (1)", exact: true })
    .click();
  await expect(pending).toHaveCount(1);
  await expect(pending).toContainText(second.body);
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page
    .locator(".sidebar")
    .getByRole("button", { name: "Inbox", exact: true })
    .click();
  await page.locator(".pr-list-item").first().click();
  await expect(
    page.getByRole("complementary", { name: "AI reviewer", exact: true }),
  ).toBeVisible();
  await expect(findings.locator("article")).toHaveCount(1);
  await findings.locator(".ai-dismissed-findings > summary").click();
  await findings
    .getByRole("button", { name: "Restore finding", exact: true })
    .click();
  await expect(findings.locator("article")).toHaveCount(2);
  await expect(pending).toHaveCount(1);
  expect(
    await page.evaluate(() =>
      JSON.stringify(
        Object.fromEntries(
          Object.entries(localStorage).filter(([key]) =>
            key.startsWith("mier.ai-review."),
          ),
        ),
      ),
    ),
  ).not.toContain(finding.body);
  current = review({
    id: "review-next",
    state: "completed",
    findings: [finding],
  });
  await page
    .getByRole("button", { name: "Run a new review", exact: true })
    .click();
  await expect(findings.locator("article")).toHaveCount(1);
  await expect(
    findings.getByLabel("Select all findings", { exact: true }),
  ).not.toBeChecked();
  await expect(pending).toHaveCount(1);
});

test("activity shows actual tool outcomes, file reads and provider usage without inventing dollar costs", async ({
  desktop,
  page,
}) => {
  let current = review({
    activityTotals: {
      toolCalls: 4,
      failedCalls: 1,
      fileReads: 1,
      filesRead: 1,
      linesRead: 12,
      bytesRead: 1250,
      omitted: 1,
    },
    activity: [
      {
        id: "tool-2",
        tool: "read_review_file",
        state: "completed",
        path: "src/feature.ts",
        side: "new",
        startLine: 1,
        endLine: 20,
        lines: 12,
        bytes: 1250,
        startedAt: "2026-10-14T12:00:00Z",
        finishedAt: "2026-10-14T12:00:01Z",
        durationMs: 1000,
      },
      {
        id: "tool-3",
        tool: "read_review_file",
        state: "failed",
        path: "src/missing.ts",
        lines: 0,
        bytes: 0,
        startedAt: "2026-10-14T12:00:01Z",
        finishedAt: "2026-10-14T12:00:01Z",
        durationMs: 10,
      },
      {
        id: "tool-4",
        tool: "finish_review",
        state: "running",
        lines: 0,
        bytes: 0,
        startedAt: "2026-10-14T12:00:02Z",
        durationMs: 0,
      },
    ],
    usage: {
      source: "copilot-events",
      partial: true,
      inputTokens: 1250,
      outputTokens: 250,
      cachedInputTokens: 500,
      premiumRequests: 0,
      nanoAiu: 44.5,
      billingStatus: "partial",
    },
  });
  desktop.handlers.set("main.AIService.StartReview", () => current);
  desktop.handlers.set("main.AIService.GetReview", () => current);
  desktop.handlers.set("main.AIService.SavePreferences", () => null);
  await openReview(desktop);
  await page
    .getByRole("button", { name: "Start AI review", exact: true })
    .click();
  const activity = page.getByRole("group", {
    name: "Review activity and usage",
    exact: true,
  });
  await activity.locator("summary").click();
  const metric = (name: string) =>
    activity
      .locator("dt")
      .filter({ hasText: new RegExp(`^${name}$`) })
      .locator("..")
      .locator("dd");
  await expect(metric("Input tokens")).toHaveText("1,250");
  await expect(metric("Dollar cost")).toHaveText("Not reported");
  await expect(metric("Premium requests")).toHaveText("0");
  await expect(activity).toContainText("Nano-AI units (subtotal)");
  await expect(activity).toContainText("Provider-reported subtotals");
  await expect(activity).toContainText("src/feature.ts:1–20 · Changed");
  await expect(activity).toContainText("12 lines returned · 1,250 bytes");
  await expect(activity).toContainText("1 unique file · 12 source lines");
  await expect(activity.locator('li[data-state="failed"]')).toContainText(
    "src/missing.ts",
  );
  await expect(activity.locator('li[data-state="running"]')).toContainText(
    "Write summary",
  );
  await expect(activity).toContainText("1 earlier call omitted");
  current = {
    ...current,
    state: "completed",
    finishedAt: "2026-10-14T12:00:03Z",
    activity: current.activity!.map((call) => ({
      ...call,
      state: "completed",
      durationMs: 1000,
    })),
    usage: {
      ...current.usage!,
      source: "copilot-session",
      partial: false,
      premiumRequests: 0.5,
    },
  };
  await expect(metric("Premium requests")).toHaveText("0.5");
  await expect(metric("Review time")).toHaveText("3.0 s");
  await expect(activity.locator('li[data-state="running"]')).toHaveCount(0);
  current = review({
    id: "without-usage",
    state: "completed",
    provider: "external",
    findings: [],
  });
  await page
    .getByRole("button", { name: "Run a new review", exact: true })
    .click();
  await expect(metric("Input tokens")).toHaveText("Not reported");
  await expect(metric("Dollar cost")).toHaveText("Not reported");
  await expect(activity).toContainText("Tool activity is unavailable");
  await expect(metric("Premium requests")).toHaveCount(0);
});

test("the AI pane sits beside the editor and stacks at narrow sizes while preserving the code", async ({
  desktop,
  page,
}) => {
  await openReview(desktop);
  const initialPane = (await page
    .getByRole("complementary", { name: "AI reviewer" })
    .boundingBox())!;
  const initialComposer = (await page
    .locator(".ai-chat-composer")
    .boundingBox())!;
  expect(initialComposer.y + initialComposer.height).toBeLessThanOrEqual(
    initialPane.y + initialPane.height + 1,
  );
  await page
    .getByRole("button", { name: "Expand code area", exact: true })
    .click();
  const pane = page.getByRole("complementary", { name: "AI reviewer" }),
    code = page.locator(".ai-review-code");
  const normal = await pane.boundingBox(),
    editor = await code.boundingBox();
  expect(normal!.x).toBeGreaterThanOrEqual(editor!.x + editor!.width - 1);
  await page.setViewportSize({ width: 980, height: 760 });
  const narrow = await pane.boundingBox(),
    narrowEditor = await code.boundingBox();
  expect(narrow!.y).toBeGreaterThanOrEqual(
    narrowEditor!.y + narrowEditor!.height - 1,
  );
  expect(narrowEditor!.height).toBeGreaterThan(130);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await pane.getByRole("button", { name: "Close AI review pane" }).click();
  await expect(page.locator(".review-workspace")).toBeVisible();
});

test("stopping preserves partial suggestions and provider failures remain retryable", async ({
  desktop,
  page,
}) => {
  let current = review({ provider: "copilot", findings: [finding] });
  desktop.handlers.set("main.AIService.StartReview", () => current);
  desktop.handlers.set("main.AIService.GetReview", () => current);
  desktop.handlers.set("main.AIService.SavePreferences", () => null);
  desktop.handlers.set("main.AIService.CancelReview", (args) => {
    expect(args).toEqual(["review-a"]);
    current = {
      ...current,
      state: "cancelled",
      error: "Review cancelled. Existing suggestions have been kept.",
    };
    return null;
  });
  await openReview(desktop);
  await page.getByLabel("AI review provider").selectOption("copilot");
  await page
    .getByRole("button", { name: "Start AI review", exact: true })
    .click();
  await expect(
    page.getByRole("region", {
      name: "Pending comment on line 1",
      exact: true,
    }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Stop review", exact: true }).click();
  await expect(
    page.getByRole("complementary", { name: "AI reviewer" }),
  ).toContainText("cancelled");
  await page
    .getByRole("article", { name: "AI finding at src/feature.ts:1" })
    .getByRole("button", { name: "Add to review", exact: true })
    .click();
  await expect(
    page.getByRole("region", {
      name: "Pending comment on line 1",
      exact: true,
    }),
  ).toBeVisible();
  desktop.handlers.set("main.AIService.StartReview", () => {
    throw new Error("Provider subscription limit reached. Retry later.");
  });
  await page
    .getByLabel("Review focus (optional)")
    .fill("Keep my focus for the retry");
  await page
    .getByRole("button", { name: "Run a new review", exact: true })
    .click();
  await expect(
    page
      .getByRole("complementary", { name: "AI reviewer" })
      .getByRole("alert")
      .first(),
  ).toContainText("subscription limit");
  await expect(page.getByLabel("Review focus (optional)")).toHaveValue(
    "Keep my focus for the retry",
  );
  await expect(
    page.getByRole("button", { name: "Run a new review", exact: true }),
  ).toBeEnabled();
});

test("external MCP connections require an explicit start and restore without persisting their bearer", async ({
  desktop,
  page,
}) => {
  let current = review({ provider: "external" });
  const connection = (): AIConnection => ({
    run: current,
    url: "http://127.0.0.1:12345/mcp",
    token: "fixture-mcp-secret",
    tools: [
      "get_review_context",
      "read_review_file",
      "add_review_comment",
      "finish_review",
    ],
  });
  desktop.handlers.set("main.AIService.StartMCPReview", () => connection());
  desktop.handlers.set("main.AIService.GetMCPConnection", () => connection());
  desktop.handlers.set("main.AIService.GetReview", () => current);
  desktop.handlers.set("main.AIService.CancelReview", () => {
    current = { ...current, state: "cancelled" };
    return null;
  });
  await openReview(desktop);
  await page
    .getByRole("button", { name: "Connect an MCP client…", exact: true })
    .click();
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".StartMCPReview")),
  ).toHaveLength(0);
  await page
    .getByRole("button", { name: "Create MCP connection", exact: true })
    .click();
  await expect(
    page.getByText("MCP connection ready", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page
    .locator(".sidebar")
    .getByRole("button", { name: "Inbox", exact: true })
    .click();
  await page.locator(".pr-list-item").first().click();
  await expect(
    page.getByRole("complementary", { name: "AI reviewer", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("MCP connection ready", { exact: true }),
  ).toBeVisible();
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".StartMCPReview")),
  ).toHaveLength(1);
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain(
    "fixture-mcp-secret",
  );
  await page.getByRole("button", { name: "Stop review", exact: true }).click();
  await expect(
    page.getByText("MCP connection ready", { exact: true }),
  ).not.toBeVisible();
});

test("late review starts survive navigation and older findings become outdated after accepting new commits", async ({
  desktop,
  page,
}) => {
  let release!: () => void,
    current = review();
  desktop.handlers.set("main.AIService.StartReview", async () => {
    await new Promise<void>((resolve) => {
      release = resolve;
    });
    return current;
  });
  desktop.handlers.set("main.AIService.GetReview", () => current);
  desktop.handlers.set("main.AIService.SavePreferences", () => null);
  await openReview(desktop);
  await page
    .getByRole("button", { name: "Start AI review", exact: true })
    .click();
  await expect.poll(() => typeof release).toBe("function");
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  release();
  await expect
    .poll(() =>
      page.evaluate(() =>
        Object.keys(localStorage).some((key) =>
          key.startsWith("mier.ai-review."),
        ),
      ),
    )
    .toBe(true);
  await page
    .locator(".sidebar")
    .getByRole("button", { name: "Inbox", exact: true })
    .click();
  await page.locator(".pr-list-item").first().click();
  await expect(
    page.getByRole("complementary", { name: "AI reviewer", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Stop review", exact: true }),
  ).toBeVisible();
  desktop.currentDetail = detail(changedSHA);
  desktop.prs = [summary(changedSHA)];
  await desktop.emit({ pullRequest: desktop.currentDetail });
  await page
    .getByRole("button", { name: "Refresh changes", exact: true })
    .click();
  current = review({
    state: "completed",
    findings: [finding],
    summary: "Reviewed an earlier revision.",
  });
  await expect(
    page.getByRole("complementary", { name: "AI reviewer" }),
  ).toContainText("earlier commit");
  await expect(page.locator(".review-outdated-comment")).toHaveCount(0);
  await page
    .getByRole("article", { name: "AI finding at src/feature.ts:1" })
    .getByRole("button", { name: "Add to review", exact: true })
    .click();
  await expect(page.locator(".review-outdated-comment")).toContainText(
    "Return after success",
  );
});

test("ChatGPT device sign-in exposes the code and reports an expired attempt", async ({
  desktop,
  page,
}) => {
  desktop.handlers.set("main.AIService.GetProviderStatus", (args) => ({
    provider: args[0],
    installed: true,
    executable: "/fixture/cli",
    authenticated: false,
    account: "",
    plan: "",
    models: [],
    error: "",
  }));
  desktop.handlers.set("main.AIService.BeginCodexLogin", () => ({
    loginId: "login-a",
    url: "https://auth.openai.com/codex/device",
    code: "TEST-CODE",
  }));
  desktop.handlers.set("main.AIService.GetCodexLoginStatus", () => ({
    state: "failed",
    error: "Device code expired. Start a new sign-in.",
  }));
  await openFixture(desktop);
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page
    .getByRole("button", { name: "Sign in with ChatGPT", exact: true })
    .click();
  await expect(page.getByText("TEST-CODE", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("region", { name: "AI providers" }).getByRole("alert"),
  ).toContainText("Device code expired");
  await expect(
    page.getByRole("button", { name: "Sign in with ChatGPT", exact: true }),
  ).toBeEnabled();
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain(
    "TEST-CODE",
  );
});
