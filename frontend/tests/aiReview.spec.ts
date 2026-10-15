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

test("reviews use the captured revision and add native private drafts without duplicates or GitHub writes", async ({
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
  await expect(pending).toContainText("AI suggestion · bug");
  await expect(pending).toContainText("Return after success");
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
    .getByRole("button", { name: "Add to review", exact: true })
    .click();
  await expect(pending).toHaveCount(2);
  await page
    .getByRole("button", {
      name: "Delete pending comment on line 1",
      exact: true,
    })
    .first()
    .click();
  await expect(pending).toHaveCount(1);
  await page
    .getByRole("button", { name: "Hide AI review", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Review with AI", exact: true })
    .click();
  await expect(pending).toHaveCount(1);
  await page
    .getByRole("button", { name: "Add suggestion back", exact: true })
    .click();
  await expect(pending).toHaveCount(2);
  current = review({
    state: "completed",
    findings: [finding],
    summary: "One actionable concern; tests were not run.",
  });
  await expect(
    page.getByRole("complementary", { name: "AI reviewer" }),
  ).toContainText(current.summary);
  expect(
    desktop.calls.some((call) =>
      /SubmitReview|CreateReviewComment/.test(call.name),
    ),
  ).toBe(false);
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
  ).toBeVisible();
  await page.getByRole("button", { name: "Stop review", exact: true }).click();
  await expect(
    page.getByRole("complementary", { name: "AI reviewer" }),
  ).toContainText("cancelled");
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
  await page
    .getByRole("button", { name: "Review with AI", exact: true })
    .click();
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
  await page
    .getByRole("button", { name: "Review with AI", exact: true })
    .click();
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
