import { test, expect } from "@playwright/test";
import { desktopFixture, openFixture } from "./desktopFixture";
test("workspace checkpoint empty-state decoding", async ({ page }) => {
  const fixture = await desktopFixture(page);
  await openFixture(fixture);
  await page.locator(".pr-list-item").first().click();
  await expect(page.locator(".review-workspace")).toBeVisible();
  const result = await page.evaluate(async () => {
    const data = await import("/src/data.ts" as string),
      persistence = await import("/src/workspaceState.ts" as string);
    const key = `checkpoint:${JSON.stringify([data.currentUser, "github.com", "fork", "app", 12])}`;
    const value = await persistence.loadWorkspaceState(key);
    return { key, type: typeof value, value };
  });
  expect(result.type).toBe("undefined");
  expect(fixture.pageErrors).toEqual([]);
});
