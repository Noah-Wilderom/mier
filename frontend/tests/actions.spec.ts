import { test as base, expect } from "@playwright/test";
import { desktopFixture, openFixture } from "./desktopFixture";
import type {
  Workflow,
  WorkflowRun,
  WorkflowJob,
  WorkflowJobLog,
} from "../src/actionsTypes";

const workflow: Workflow = {
  id: 4,
  name: "Tests",
  path: ".github/workflows/test.yml",
  state: "active",
  url: "https://github.com/fork/app/actions/workflows/test.yml",
};
function run(id = 12, attempt = 2, status = "completed"): WorkflowRun {
  return {
    id,
    workflowId: 4,
    name: "Tests",
    displayTitle: `Review workflow ${id}`,
    runNumber: id,
    runAttempt: attempt,
    status,
    conclusion: status === "completed" ? "failure" : "",
    headBranch: "feature",
    headSha: "b".repeat(40),
    event: "pull_request",
    createdAt: "2026-10-14T10:00:00Z",
    updatedAt: "2026-10-14T10:02:00Z",
    runStartedAt: "2026-10-14T10:00:00Z",
    url: `https://github.com/fork/app/actions/runs/${id}`,
    actor: { login: "reviewer", name: "Reviewer", avatarUrl: "", url: "" },
  };
}
function job(id = 99, attempt = 2, status = "completed"): WorkflowJob {
  return {
    id,
    runId: 12,
    runAttempt: attempt,
    name: `Package linux ${id}`,
    status,
    conclusion: status === "completed" ? "failure" : "",
    startedAt: "2026-10-14T10:00:00Z",
    completedAt: status === "completed" ? "2026-10-14T10:02:00Z" : "",
    url: `https://github.com/fork/app/actions/runs/12/job/${id}`,
    checkRunUrl: "https://api.github.com/repos/fork/app/check-runs/321",
    runnerName: "ubuntu-26.04",
    steps: [
      {
        number: 1,
        name: "Build application",
        status,
        conclusion: status === "completed" ? "failure" : "",
        startedAt: "2026-10-14T10:00:00Z",
        completedAt: "",
      },
    ],
  };
}
function log(value = job()): WorkflowJobLog {
  return {
    job: value,
    state: "available",
    message: "",
    text: `##[group]Build application\n\x1b[31m##[error]Failure in attempt ${value.runAttempt}\x1b[0m\n<script>literal output</script>\n##[endgroup]\n`,
    truncated: false,
    tail: true,
  };
}
const test = base.extend<{
  desktop: Awaited<ReturnType<typeof desktopFixture>>;
}>({
  desktop: async ({ page }, use) => {
    const desktop = await desktopFixture(page);
    desktop.handlers.set("main.GitHubService.ListWorkflows", () => [workflow]);
    desktop.handlers.set("main.GitHubService.ListWorkflowRuns", () => ({
      items: [run()],
      totalCount: 1,
      hasMore: false,
    }));
    desktop.handlers.set("main.GitHubService.GetWorkflowRun", (args) => ({
      run: run(args[3], args[4] || 2),
      jobs: [job(args[4] === 1 ? 98 : 99, args[4] || 2)],
    }));
    desktop.handlers.set("main.GitHubService.GetWorkflowJobLog", (args) =>
      log(job(args[3], args[3] === 98 ? 1 : 2)),
    );
    desktop.handlers.set("main.GitHubService.GetCheckWorkflowJob", () => job());
    await use(desktop);
    expect(desktop.unexpected).toEqual([]);
    expect(desktop.pageErrors).toEqual([]);
  },
});
async function openActions(
  desktop: Awaited<ReturnType<typeof desktopFixture>>,
) {
  await openFixture(desktop);
  await expect(
    desktop.page
      .locator(".sidebar")
      .getByRole("button", { name: "Actions", exact: true }),
  ).toHaveCount(0);
  await desktop.page.keyboard.press("Control+4");
  await expect(
    desktop.page.getByRole("heading", { name: "Actions", exact: true }),
  ).toBeVisible();
}

test("Actions loads real job logs, step groups and safe searchable output", async ({
  desktop,
  page,
}) => {
  await openActions(desktop);
  await page.getByRole("button", { name: /Review workflow 12/ }).click();
  const logs = page.getByRole("region", { name: "Logs for Package linux 99" });
  await expect(logs).toContainText("Failure in attempt 2");
  await expect(
    logs.locator(".actions-log-section.has-error summary"),
  ).toContainText("Build application");
  await expect(logs.locator("script")).toHaveCount(0);
  await expect(logs).toContainText("<script>literal output</script>");
  await expect(logs).not.toContainText("\x1b");
  await logs.getByLabel("Search job logs").fill("missing search");
  await expect(logs).toContainText("No log sections match");
  await logs.getByLabel("Search job logs").fill("failure");
  await expect(logs.locator(".actions-log-output")).toContainText(
    "Failure in attempt 2",
  );
  const request = desktop.calls.find((call) =>
    call.name.endsWith(".GetWorkflowJobLog"),
  )!;
  expect(request.args).toEqual(["github.com", "fork", "app", 99]);
});

test("Jev classifies a job failure without rerunning or cancelling it", async ({
  desktop,
  page,
}) => {
  desktop.handlers.set("main.PredictionService.GetStatus", () => ({
    configured: true,
    enabled: true,
    model: "jev-1.13.0",
    error: "",
  }));
  desktop.handlers.set("main.PredictionService.Evaluate", (args) => {
    expect(args[0]).toEqual({
      kind: "ci",
      scope: { host: "github.com", owner: "fork", repo: "app" },
      jobId: 99,
    });
    return {
      id: "ci-a",
      kind: "ci",
      model: "jev-1.13.0",
      questionVersion: "mier-jev-1",
      createdAt: new Date().toISOString(),
      cached: false,
      durationMs: 200,
      usage: { input_tokens: 250, output_tokens: 0 },
      warnings: [],
      rows: [
        {
          id: "ci_category",
          label: "Likely failure category",
          answer: {
            type: "choice",
            choice: "build",
            confidence: 0.8,
            probabilities: { build: 0.9, unknown: 0.1 },
          },
          options: {
            build: "Compiler, type checker, linter or packaging error",
            unknown: "Insufficient evidence",
          },
        },
      ],
    };
  });
  await openActions(desktop);
  await page.getByRole("button", { name: /Review workflow 12/ }).click();
  await page
    .getByRole("button", { name: "Assess job failure with Jev" })
    .click();
  const panel = page.getByRole("region", {
    name: "Assess job failure with Jev",
  });
  await panel
    .getByRole("button", { name: "Start assessment", exact: true })
    .click();
  await expect(panel).toContainText(
    "Compiler, type checker, linter or packaging error",
  );
  await expect(page.locator(".actions-log-output")).toContainText(
    "Failure in attempt 2",
  );
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".MutateWorkflowRun")),
  ).toHaveLength(0);
});

test("run filters, pagination and retries preserve useful results", async ({
  desktop,
  page,
}) => {
  let fail = false;
  desktop.handlers.set("main.GitHubService.ListWorkflowRuns", (args) => {
    if (fail) throw new Error("GitHub rate limit is cooling down; retry later");
    return {
      items: [run(args[3].page === 2 ? 13 : 12)],
      totalCount: 40,
      hasMore: args[3].page === 1,
    };
  });
  await openActions(desktop);
  await page.getByLabel("Workflow", { exact: true }).selectOption("4");
  await page.getByLabel("Run status", { exact: true }).selectOption("failure");
  await page.getByLabel("Filter runs by branch").fill("feature/test");
  await page.getByRole("button", { name: "Filter runs", exact: true }).click();
  await expect
    .poll(
      () =>
        desktop.calls
          .filter((call) => call.name.endsWith(".ListWorkflowRuns"))
          .at(-1)?.args[3],
    )
    .toEqual({
      workflowId: 4,
      status: "failure",
      branch: "feature/test",
      page: 1,
    });
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(
    page.getByRole("button", { name: /Review workflow 13/ }),
  ).toBeVisible();
  fail = true;
  await page
    .getByRole("button", { name: "Refresh Actions", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("rate limit");
  await expect(
    page.getByRole("button", { name: /Review workflow 13/ }),
  ).toBeVisible();
  fail = false;
  await page.getByRole("alert").getByRole("button", { name: "Retry" }).click();
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("earlier attempts use their original job IDs and cannot change a newer run", async ({
  desktop,
  page,
}) => {
  await openActions(desktop);
  await page.getByRole("button", { name: /Review workflow 12/ }).click();
  await expect(
    page.getByRole("region", { name: "Logs for Package linux 99" }),
  ).toContainText("Failure in attempt 2");
  await page.getByLabel("Run attempt").selectOption("1");
  await expect(
    page.getByRole("region", { name: "Logs for Package linux 98" }),
  ).toContainText("Failure in attempt 1");
  await expect(page.getByText("Viewing an earlier attempt.")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Rerun all jobs", exact: true }),
  ).toHaveCount(0);
  await page.getByLabel("Run attempt").selectOption("0");
  await expect(
    page.getByRole("region", { name: "Logs for Package linux 99" }),
  ).toContainText("Failure in attempt 2");
});

test("rerun requires target confirmation, sends the expected attempt and reports errors", async ({
  desktop,
  page,
}) => {
  desktop.handlers.set("main.GitHubService.MutateWorkflowRun", (args) => {
    expect(args).toEqual([
      "github.com",
      "fork",
      "app",
      { action: "rerun-failed", runId: 12, expectedAttempt: 2, jobId: 99 },
    ]);
    throw new Error("This run has a newer attempt. Refresh before changing it");
  });
  await openActions(desktop);
  await page.getByRole("button", { name: /Review workflow 12/ }).click();
  await expect(
    page.getByRole("region", { name: "Logs for Package linux 99" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Rerun failed jobs", exact: true })
    .click();
  const modal = page.getByRole("dialog", { name: "Rerun failed jobs" });
  await expect(modal).toContainText("Tests #12");
  await expect(modal).toContainText("fork/app · feature · bbbbbbb");
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".MutateWorkflowRun")),
  ).toHaveLength(0);
  await modal
    .getByRole("button", { name: "Rerun failed jobs", exact: true })
    .click();
  await expect(modal.getByRole("alert")).toContainText("newer attempt");
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".MutateWorkflowRun")),
  ).toHaveLength(1);
  await modal.getByRole("button", { name: "Back", exact: true }).click();
  await expect(modal).not.toBeVisible();
});

test("pending logs poll without overlapping and download once a job completes", async ({
  desktop,
  page,
}) => {
  await page.clock.install();
  desktop.handlers.set("main.GitHubService.GetWorkflowRun", () => ({
    run: run(12, 2, "in_progress"),
    jobs: [job(99, 2, "in_progress")],
  }));
  let complete = false;
  desktop.handlers.set("main.GitHubService.GetWorkflowJobLog", () =>
    complete
      ? log()
      : {
          ...log(),
          state: "pending",
          text: "",
          message: "Logs become available when this job completes.",
        },
  );
  await openActions(desktop);
  await page.getByRole("button", { name: /Review workflow 12/ }).click();
  const logs = page.getByRole("region", { name: "Logs for Package linux 99" });
  await expect(logs).toContainText("Logs become available");
  complete = true;
  await page.clock.fastForward(16000);
  await expect(logs).toContainText("Failure in attempt 2");
  await expect(
    page.getByRole("button", { name: "Cancel run", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Rerun all jobs", exact: true }),
  ).toHaveCount(0);
});

test("PR checks fetch Actions logs separately and keep external provider summaries", async ({
  desktop,
  page,
}) => {
  desktop.currentDetail.checks = [
    {
      id: 321,
      name: "Package linux",
      status: "completed",
      conclusion: "failure",
      startedAt: "",
      completedAt: "",
      detailsUrl: "https://github.com/fork/app/actions/runs/12/job/99",
      app: { slug: "github-actions" },
      output: { title: "", summary: "", text: "" },
    },
    {
      id: 322,
      name: "External quality",
      status: "completed",
      conclusion: "skipped",
      startedAt: "",
      completedAt: "",
      detailsUrl: "https://quality.example.test/123",
      app: { slug: "quality" },
      output: {
        title: "",
        summary: "## Quality summary\nNo findings.",
        text: "",
      },
    },
  ];
  await openFixture(desktop);
  await page.locator(".pr-list-item").first().click();
  await page.getByRole("tab", { name: /Checks/ }).click();
  await page.getByRole("button", { name: /Package linux/ }).click();
  await expect(
    page.getByRole("region", { name: "Logs for Package linux 99" }),
  ).toContainText("Failure in attempt 2");
  await expect(page.locator(".checks-pane")).not.toContainText("Sample checks");
  await page.getByRole("button", { name: /External quality/ }).click();
  await expect(
    page.getByRole("heading", { name: "Quality summary" }),
  ).toBeVisible();
  await expect(page.locator(".check-row")).toContainText(["Skipped"]);
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".GetCheckWorkflowJob")),
  ).toHaveLength(1);
});

test("manual workflow runs use typed declared inputs and selected ref with one write", async ({
  desktop,
  page,
}) => {
  desktop.handlers.set("main.GitHubService.GetWorkflowDispatch", (args) => ({
    workflow,
    ref: args[4] || "main",
    enabled: true,
    inputs: [
      {
        name: "target",
        description: "Build target",
        type: "choice",
        required: true,
        default: "linux",
        options: ["linux", "macos"],
      },
      {
        name: "debug",
        description: "",
        type: "boolean",
        required: false,
        default: "false",
        options: [],
      },
    ],
  }));
  desktop.handlers.set("main.GitHubService.DispatchWorkflow", (args) => {
    expect(args).toEqual([
      "github.com",
      "fork",
      "app",
      {
        workflowId: 4,
        ref: "feature",
        inputs: { target: "macos", debug: "true" },
      },
    ]);
  });
  await openActions(desktop);
  await page.getByLabel("Workflow", { exact: true }).selectOption("4");
  await page.getByRole("button", { name: "Run workflow", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "Run Tests", exact: true });
  await expect(modal.getByLabel("Workflow branch or tag")).toHaveValue("main");
  await modal.getByLabel("Workflow branch or tag").fill("feature");
  await expect(
    modal.getByRole("button", { name: "Run workflow", exact: true }),
  ).toBeDisabled();
  await modal.getByRole("button", { name: "Load inputs for this ref" }).click();
  await expect(
    modal.getByRole("button", { name: "Run workflow", exact: true }),
  ).toBeEnabled();
  await modal.getByLabel("target", { exact: true }).selectOption("macos");
  await modal.getByLabel("debug", { exact: true }).selectOption("true");
  await modal
    .getByRole("button", { name: "Run workflow", exact: true })
    .click();
  await expect(modal).not.toBeVisible();
  await expect(
    page.getByText("Workflow requested.", { exact: false }),
  ).toBeVisible();
  expect(
    desktop.calls.filter((call) => call.name.endsWith(".DispatchWorkflow")),
  ).toHaveLength(1);
});

test("switching selected runs discards a delayed job response", async ({
  desktop,
  page,
}) => {
  let resolve!: (value: unknown) => void;
  desktop.handlers.set("main.GitHubService.ListWorkflowRuns", () => ({
    items: [run(12), run(13)],
    totalCount: 2,
    hasMore: false,
  }));
  desktop.handlers.set("main.GitHubService.GetWorkflowRun", (args) =>
    args[3] === 12
      ? new Promise((value) => {
          resolve = value;
        })
      : { run: run(13), jobs: [job(100)] },
  );
  await openActions(desktop);
  await page.getByRole("button", { name: /Review workflow 12/ }).click();
  await expect.poll(() => !!resolve).toBe(true);
  await page.getByRole("button", { name: /Review workflow 13/ }).click();
  await expect(
    page.getByRole("region", { name: "Logs for Package linux 100" }),
  ).toBeVisible();
  resolve({ run: run(12), jobs: [job(99)] });
  await expect(
    page.getByRole("region", { name: "Workflow run 13" }),
  ).toBeVisible();
  await expect(
    page.getByRole("region", { name: "Logs for Package linux 99" }),
  ).toHaveCount(0);
});

test("the newest observed attempt remains selectable after opening older logs", async ({
  desktop,
  page,
}) => {
  desktop.handlers.set("main.GitHubService.GetWorkflowRun", (args) => ({
    run: run(12, args[4] || 3),
    jobs: [job(99, args[4] || 3)],
  }));
  await openActions(desktop);
  await page.getByRole("button", { name: /Review workflow 12/ }).click();
  const attempt = page.getByLabel("Run attempt");
  await expect(attempt.locator('option[value="0"]')).toHaveText(
    "Latest (attempt 3)",
  );
  await attempt.selectOption("1");
  await expect(attempt.locator('option[value="0"]')).toHaveText(
    "Latest (attempt 3)",
  );
  await expect(attempt.locator('option[value="3"]')).toHaveCount(1);
  await expect(page.getByText("Viewing an earlier attempt.")).toBeVisible();
});

test("polling waits for the current request and pauses while the view is hidden", async ({
  desktop,
  page,
}) => {
  await page.clock.install();
  let resolve!: (value: unknown) => void,
    requests = 0;
  desktop.handlers.set("main.GitHubService.ListWorkflowRuns", () => {
    requests++;
    if (requests === 1)
      return new Promise((value) => {
        resolve = value;
      });
    return { items: [run()], totalCount: 1, hasMore: false };
  });
  await openActions(desktop);
  await expect.poll(() => requests).toBe(1);
  await page.clock.fastForward(45000);
  expect(requests).toBe(1);
  resolve({ items: [run()], totalCount: 1, hasMore: false });
  await expect(
    page.getByRole("button", { name: /Review workflow 12/ }),
  ).toBeVisible();
  await page.evaluate(() => {
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => "hidden",
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.clock.fastForward(16000);
  expect(requests).toBe(1);
  await page.evaluate(() => {
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => "visible",
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await expect.poll(() => requests).toBe(2);
});

test("artifacts use explicit native downloads and comparisons keep both run attempts", async ({
  desktop,
  page,
}) => {
  let duplicateJobs = false;
  desktop.handlers.set("main.GitHubService.ListWorkflowArtifacts", () => ({
    items: [
      {
        id: 7,
        name: "desktop-build",
        sizeInBytes: 1048576,
        expired: false,
        createdAt: "2026-10-14T10:00:00Z",
        expiresAt: "2026-11-14T10:00:00Z",
        digest: "",
      },
      {
        id: 8,
        name: "expired-build",
        sizeInBytes: 1,
        expired: true,
        createdAt: "2026-10-14T10:00:00Z",
        expiresAt: "2026-10-15T10:00:00Z",
        digest: "",
      },
    ],
    hasMore: false,
  }));
  desktop.handlers.set(
    "main.GitHubService.DownloadWorkflowArtifact",
    () => "/fixture/downloads/build.zip",
  );
  desktop.handlers.set("main.GitHubService.GetWorkflowRun", (args) => {
    const attempt = args[4] || 2;
    return {
      run: {
        ...run(args[3], attempt),
        conclusion: attempt === 1 ? "failure" : "success",
      },
      jobs: [
        {
          ...job(attempt === 1 ? 98 : 99, attempt),
          name: "Tests",
          conclusion: attempt === 1 ? "failure" : "success",
          steps: [
            {
              number: 1,
              name: "Build application",
              status: "completed",
              conclusion: attempt === 1 ? "failure" : "success",
              startedAt: "",
              completedAt: "",
            },
          ],
        },
        ...(duplicateJobs && attempt === 1
          ? [
              {
                ...job(198, 1),
                name: "Tests",
                conclusion: "cancelled",
                steps: [],
              },
            ]
          : []),
      ],
    };
  });
  await openActions(desktop);
  await page.getByRole("button", { name: /Review workflow 12/ }).click();
  expect(
    desktop.calls.some((call) => call.name.endsWith(".ListWorkflowArtifacts")),
  ).toBe(false);
  await page.getByText("Artifacts", { exact: true }).click();
  const artifacts = page.locator(".workflow-artifacts");
  await expect(artifacts).toContainText("desktop-build");
  await expect(
    artifacts
      .locator(".workflow-artifact")
      .filter({ hasText: "expired-build" })
      .getByRole("button"),
  ).toBeDisabled();
  await artifacts
    .locator(".workflow-artifact")
    .filter({ hasText: "desktop-build" })
    .getByRole("button", { name: "Download ZIP" })
    .click();
  await expect(artifacts).toContainText(
    "Saved desktop-build to /fixture/downloads/build.zip",
  );
  expect(
    desktop.calls.find((call) =>
      call.name.endsWith(".DownloadWorkflowArtifact"),
    )?.args[3],
  ).toMatchObject({ artifactId: 7, runId: 12, headSha: "b".repeat(40) });
  await page.getByText("Compare runs and attempts", { exact: true }).click();
  await page.getByLabel("Compare workflow baseline").selectOption("12:1");
  const comparison = page.locator(".run-comparison");
  await expect(
    comparison.getByRole("cell", { name: "failure", exact: true }),
  ).toBeVisible();
  await expect(
    comparison.getByRole("cell", { name: "success", exact: true }),
  ).toBeVisible();
  await expect(comparison).toContainText("Build application → None");
  duplicateJobs = true;
  await page.getByLabel("Compare workflow baseline").selectOption("");
  await page.getByLabel("Compare workflow baseline").selectOption("12:1");
  await expect(comparison).toContainText("Repeated job names are grouped");
  await expect(
    comparison.getByRole("cell", {
      name: "failure · #98; cancelled · #198",
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    comparison.getByRole("cell", { name: "success · #99", exact: true }),
  ).toBeVisible();
  await expect(
    comparison.getByRole("cell", { name: "—", exact: true }),
  ).toBeVisible();
  await page.getByLabel("Run attempt").selectOption("1");
  await expect(page.getByLabel("Compare workflow baseline")).toHaveCount(0);
  await page.getByText("Compare runs and attempts", { exact: true }).click();
  await expect(page.getByLabel("Compare workflow baseline")).toHaveValue("");
  await page.getByLabel("Compare workflow baseline").selectOption("12:2");
  await expect(comparison).toContainText("success");
});
