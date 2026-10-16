import { test as base, expect } from "@playwright/test";
import { desktopFixture, openFixture, root, headSHA } from "./desktopFixture";
import type {
  AIActionRequest,
  AIActionSnapshot,
  AIWorktreeInfo,
} from "../src/aiActionTypes";
import type { ConflictFile } from "../src/gitOperations";

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
function result(
  request: AIActionRequest,
  overrides: Partial<AIActionSnapshot> = {},
): AIActionSnapshot {
  return {
    kind: request.kind,
    root: request.root,
    writes: 0,
    proposal: { text: "feat: validate source input" },
    run: {
      id: "action-fixture",
      key: "fixture",
      provider: request.provider,
      model: request.model,
      intensity: request.intensity,
      instructions: request.instructions,
      headSha: headSHA,
      state: "completed",
      progress: "",
      summary: "Captured changes inspected.",
      error: "",
      findings: [],
      version: 1,
      createdAt: new Date().toISOString(),
      finishedAt: new Date().toISOString(),
      warnings: [],
      activity: [
        {
          id: "read-1",
          tool: "read_task_file",
          path: "src/feature.ts",
          state: "completed",
          lines: 1,
          bytes: 24,
          startedAt: new Date().toISOString(),
          durationMs: 20,
        },
      ],
      activityTotals: {
        toolCalls: 3,
        failedCalls: 0,
        fileReads: 1,
        filesRead: 1,
        linesRead: 1,
        bytesRead: 24,
        omitted: 0,
      },
      usage: {
        source: "fixture",
        partial: false,
        inputTokens: 30,
        outputTokens: 12,
      },
    },
    ...overrides,
  };
}
async function openCommit(desktop: Awaited<ReturnType<typeof desktopFixture>>) {
  const repo = desktop.repositories.get(root)!;
  repo.changes = repo.changes!.map((file) => ({
    ...file,
    staged: true,
    unstaged: false,
    indexStatus: "M",
    worktreeStatus: ".",
  }));
  await openFixture(desktop);
  await desktop.page
    .locator(".sidebar")
    .getByRole("button", { name: /Changes/ })
    .click();
  await desktop.page
    .getByLabel("Commit message", { exact: true })
    .fill("My human draft");
  await desktop.page
    .getByRole("button", { name: "Draft with AI", exact: true })
    .click();
  return desktop.page.getByRole("dialog", {
    name: "Draft a commit message",
    exact: true,
  });
}

test("commit drafting uses assistant-ui, provider logos, intensity and explicit validated use", async ({
  desktop,
  page,
}) => {
  let current: AIActionSnapshot;
  desktop.handlers.set("main.AIService.StartAction", (args) => {
    expect(args[0]).toMatchObject({
      root,
      kind: "commit",
      expectedHead: headSHA,
      provider: "copilot",
      intensity: "high",
      instructions: "Focus on validation",
    });
    current = result(args[0]);
    return current;
  });
  desktop.handlers.set("main.AIService.GetAction", () => current);
  desktop.handlers.set("main.AIService.ValidateAction", () => null);
  const dialog = await openCommit(desktop);
  await dialog.getByLabel("AI task provider").selectOption("copilot");
  await expect(dialog.locator(".ai-provider-select img")).toHaveAttribute(
    "src",
    "/ai/githubcopilot.svg",
  );
  await dialog.getByRole("radio", { name: "Thorough", exact: true }).check();
  await dialog
    .getByLabel("Instructions (optional)")
    .fill("Focus on validation");
  await dialog.getByLabel("Instructions (optional)").press("Enter");
  await expect(dialog.getByLabel("AI proposal")).toContainText(
    "feat: validate source input",
  );
  await expect(page.getByLabel("Commit message", { exact: true })).toHaveValue(
    "My human draft",
  );
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".ValidateAction")),
  ).toHaveLength(0);
  await dialog.getByLabel("Task activity and usage").locator("summary").click();
  await expect(dialog.getByLabel("Executed task tools")).toContainText(
    "Read captured file",
  );
  await expect(dialog).toContainText("30");
  await dialog
    .getByRole("button", { name: "Use commit draft", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByLabel("Commit message", { exact: true })).toHaveValue(
    "feat: validate source input",
  );
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".ValidateAction")),
  ).toHaveLength(1);
  expect(
    desktop.calls.some((call) =>
      /RepositoryService\.(Commit|Amend)$/.test(call.name),
    ),
  ).toBe(false);
});

test("a stale commit proposal preserves the human draft and can be regenerated", async ({
  desktop,
  page,
}) => {
  desktop.handlers.set("main.AIService.StartAction", (args) => result(args[0]));
  desktop.handlers.set("main.AIService.ValidateAction", () => {
    throw new Error("Staged changes moved. Generate a fresh proposal");
  });
  const dialog = await openCommit(desktop);
  await dialog
    .getByRole("button", { name: "Start AI task", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "Use commit draft", exact: true })
    .click();
  await expect(dialog.getByRole("alert")).toContainText("Staged changes moved");
  await expect(page.getByLabel("Commit message", { exact: true })).toHaveValue(
    "My human draft",
  );
  await expect(
    dialog.getByRole("button", { name: "Generate again", exact: true }),
  ).toBeEnabled();
});

test("closing and reopening during task preparation restores the late result without duplicate starts", async ({
  desktop,
  page,
}) => {
  let release!: () => void;
  desktop.handlers.set("main.AIService.StartAction", async (args) => {
    await new Promise<void>((resolve) => {
      release = resolve;
    });
    return result(args[0]);
  });
  const dialog = await openCommit(desktop);
  await dialog
    .getByRole("button", { name: "Start AI task", exact: true })
    .click();
  await expect.poll(() => typeof release).toBe("function");
  await dialog
    .getByRole("button", { name: "Close Draft a commit message", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Draft with AI", exact: true })
    .click();
  await expect(
    dialog.getByRole("button", { name: "Preparing task…", exact: true }),
  ).toBeDisabled();
  release();
  await expect(dialog.getByLabel("AI proposal")).toContainText(
    "feat: validate source input",
  );
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".StartAction")),
  ).toHaveLength(1);
  const stored = await page.evaluate(() => JSON.stringify(localStorage));
  expect(stored).not.toContain("feat: validate source input");
  await expect(page.getByLabel("Commit message", { exact: true })).toHaveValue(
    "My human draft",
  );
});

test("PR drafting fills only the draft after explicit use", async ({
  desktop,
  page,
}) => {
  desktop.handlers.set("main.AIService.StartAction", (args) => {
    expect(args[0]).toMatchObject({
      kind: "pull_request",
      base: "main",
      head: "feature",
      expectedHead: headSHA,
    });
    return result(args[0], {
      proposal: {
        title: "Validate source input",
        body: "Reject empty values before processing.\n\nValidation: not run.",
      },
    });
  });
  desktop.handlers.set("main.AIService.ValidateAction", () => null);
  await openFixture(desktop);
  await page
    .getByRole("button", { name: "New pull request", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Draft with AI", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "Draft a pull request",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "Start AI task", exact: true })
    .click();
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue("");
  await dialog
    .getByRole("button", { name: "Use PR draft", exact: true })
    .click();
  await expect(page.getByLabel("Title", { exact: true })).toHaveValue(
    "Validate source input",
  );
  await expect(
    page.getByRole("textbox", { name: "Description", exact: true }),
  ).toHaveValue(
    "Reject empty values before processing.\n\nValidation: not run.",
  );
  expect(
    desktop.calls.some((call) => call.name.endsWith(".CreatePullRequest")),
  ).toBe(false);
});

test("branch explanation is read only and grounded in the selected comparison", async ({
  desktop,
  page,
}) => {
  desktop.handlers.set("main.AIService.StartAction", (args) => {
    expect(args[0]).toMatchObject({
      kind: "explain",
      base: "main",
      head: "feature",
    });
    return result(args[0], {
      proposal: {
        text: "The branch validates input before processing. Check empty and malformed values.",
      },
    });
  });
  await openFixture(desktop);
  await page
    .getByRole("button", { name: "Compare branches", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Explain with AI", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "Explain branch changes",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "Start AI task", exact: true })
    .click();
  await expect(dialog.getByLabel("AI proposal")).toContainText(
    "Check empty and malformed values",
  );
  await expect(
    dialog.getByRole("button", { name: "Use proposal", exact: true }),
  ).toHaveCount(0);
});

test("AI conflict proposal stays beside the result and requires use then separate save", async ({
  desktop,
  page,
}) => {
  const repo = desktop.repositories.get(root)!;
  const version = (content: string) => ({
    content,
    exists: true,
    sha: headSHA,
    mode: "100644",
    binary: false,
  });
  const file: ConflictFile = {
    path: "src/feature.ts",
    token: "conflict-ai-1",
    editable: true,
    reason: "",
    currentLabel: "feature",
    incomingLabel: "main",
    base: version("base\n"),
    current: version("current\n"),
    incoming: version("incoming\n"),
    working: "<<<<<<< HEAD\ncurrent\n=======\nincoming\n>>>>>>> main\n",
  };
  repo.operation = {
    kind: "merge",
    token: "operation-ai",
    sourceSha: headSHA,
    currentLabel: "feature",
    sourceLabel: "main",
    conflicts: 1,
    message: "",
    canSkip: false,
  };
  repo.changes = [
    {
      path: file.path,
      status: "conflicted",
      conflict: true,
      staged: true,
      unstaged: true,
      untracked: false,
      indexStatus: "U",
      worktreeStatus: "U",
    },
  ];
  desktop.handlers.set("main.RepositoryService.Conflict", () => file);
  desktop.handlers.set("main.AIService.StartAction", (args) => {
    expect(args[0]).toMatchObject({
      kind: "conflict",
      path: file.path,
      expectedHead: headSHA,
      currentResult: file.working,
    });
    return result(args[0], {
      proposal: {
        text: "Preserves both behaviors.",
        content: "current\nincoming\n",
      },
    });
  });
  desktop.handlers.set("main.AIService.ValidateAction", () => null);
  desktop.handlers.set("main.RepositoryService.ResolveConflict", (args) => {
    expect(args[1]).toEqual({
      path: file.path,
      token: file.token,
      choice: "result",
      content: "current\nincoming\n",
    });
    repo.changes = [];
    repo.operation = undefined;
  });
  desktop.handlers.set("main.RepositoryService.Tags", () => []);
  desktop.handlers.set("main.AIService.AgentWorktrees", () => []);
  await openFixture(desktop);
  await page
    .getByRole("button", { name: "Resolve conflicts", exact: true })
    .click();
  const editor = page.getByRole("region", {
    name: "Resolve conflict in src/feature.ts",
  });
  await editor
    .getByRole("button", { name: "Propose with AI", exact: true })
    .click();
  const chat = editor.getByRole("region", {
    name: "Propose a conflict resolution",
    exact: true,
  });
  await chat
    .getByRole("button", { name: "Start AI task", exact: true })
    .click();
  await expect(
    editor.getByLabel("Merged result", { exact: true }),
  ).toContainText("<<<<<<< HEAD");
  const currentBox = await editor
      .getByLabel("Merged result", { exact: true })
      .boundingBox(),
    proposalBox = await chat.boundingBox();
  expect(proposalBox!.x).toBeGreaterThan(currentBox!.x + currentBox!.width - 2);
  await chat
    .getByRole("button", { name: "Use proposal in result", exact: true })
    .click();
  await expect(
    editor.getByLabel("Merged result", { exact: true }),
  ).not.toContainText("<<<<<<< HEAD");
  expect(
    desktop.calls.some((call) => call.name.endsWith(".ResolveConflict")),
  ).toBe(false);
  await editor
    .getByRole("button", { name: "Undo choice", exact: true })
    .click();
  await expect(
    editor.getByLabel("Merged result", { exact: true }),
  ).toContainText("<<<<<<< HEAD");
  await chat
    .getByRole("button", { name: "Use proposal in result", exact: true })
    .click();
  // A second assistant in a dialog must keep its own labelled composer while
  // the conflict assistant remains mounted behind it.
  await page
    .getByRole("button", { name: "Repository tools…", exact: true })
    .click();
  const tools = page.getByRole("dialog", {
    name: "Repository tools",
    exact: true,
  });
  await tools
    .getByRole("button", { name: "Coding agents", exact: true })
    .click();
  await tools.getByText("Coding task", { exact: true }).click();
  await expect(
    tools.getByRole("textbox", { name: "Coding task", exact: true }),
  ).toBeFocused();
  await tools
    .getByRole("textbox", { name: "Coding task", exact: true })
    .fill("Keep this coding instruction separate");
  await tools
    .getByRole("button", { name: "Close Repository tools", exact: true })
    .click();
  await expect(
    chat.getByLabel("Instructions (optional)", { exact: true }),
  ).toHaveValue("");
  await editor
    .getByRole("button", { name: "Save result and stage", exact: true })
    .click();
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".ResolveConflict")),
  ).toHaveLength(1);
});

test("coding worktrees show tool activity, retain stopped edits and confirm clean-only removal", async ({
  desktop,
  page,
}) => {
  let current: AIActionSnapshot,
    entries: AIWorktreeInfo[] = [];
  desktop.handlers.set("main.RepositoryService.Tags", () => []);
  desktop.handlers.set("main.AIService.AgentWorktrees", () => entries);
  desktop.handlers.set("main.AIService.StartAction", (args) => {
    expect(args[0]).toMatchObject({
      kind: "code",
      root,
      instructions: "Add validation",
    });
    const worktree: AIWorktreeInfo = {
      id: "agent-1",
      repositoryRoot: root,
      path: "/fixture/agent-1",
      branch: "mier/agent/first",
      headSha: headSHA,
      state: "running",
      createdAt: new Date().toISOString(),
      lastActionId: "action-fixture",
    };
    entries = [worktree];
    current = result(args[0], { worktree, writes: 1, proposal: null });
    current.run.state = "running";
    current.run.progress = "Edited src/feature.ts in the agent worktree";
    return current;
  });
  desktop.handlers.set("main.AIService.GetAction", () => current);
  desktop.handlers.set("main.AIService.CancelAction", () => {
    current.run.state = "cancelled";
    current.run.error = "Task stopped. Worktree edits were kept.";
    entries = entries.map((item) => ({ ...item, state: "cancelled" }));
    return null;
  });
  desktop.handlers.set("main.AIService.RemoveAgentWorktree", () => {
    throw new Error("The worktree has changes. Review them first");
  });
  await openFixture(desktop);
  await page
    .getByRole("button", { name: "Repository tools…", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "Repository tools",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "Coding agents", exact: true })
    .click();
  await dialog
    .getByLabel("Coding task", { exact: true })
    .fill("Add validation");
  await dialog.getByLabel("Coding task", { exact: true }).press("Enter");
  await expect(dialog).toContainText("1 file write");
  await dialog.getByRole("button", { name: "Stop task", exact: true }).click();
  await expect(
    dialog.getByRole("button", {
      name: "Open worktree to review",
      exact: true,
    }),
  ).toBeEnabled();
  await dialog
    .getByRole("navigation", { name: "Coding worktrees", exact: true })
    .getByRole("button", { name: /mier\/agent\/first/ })
    .click();
  await dialog.getByRole("button", { name: "Remove…", exact: true }).click();
  expect(
    desktop.calls.some((call) => call.name.endsWith(".RemoveAgentWorktree")),
  ).toBe(false);
  await dialog
    .getByRole("button", { name: "Remove clean worktree", exact: true })
    .click();
  await expect(
    dialog.getByRole("alert").filter({ hasText: "The worktree has changes" }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("navigation", { name: "Coding worktrees", exact: true }),
  ).toContainText("mier/agent/first");
});
