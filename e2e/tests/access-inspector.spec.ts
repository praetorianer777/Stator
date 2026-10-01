import type { Locator, Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import type { StatorApi } from "../fixtures/api";

/** A space with Plans, narrowed to alice alone, and Salaries below it, which bob cannot open. */
async function restrictedPage(api: StatorApi, key: string, name: string): Promise<{ salaries: string }> {
  const space = await createSpace(api, key, name);
  const plans = await createPage(api, space.homePageId, "Plans");
  const salaries = await createPage(api, plans.id, "Salaries");
  const alice = (await api.GET("/auth/me")).data!.user;
  const saved = await api.PUT("/pages/{pageID}/restrictions", {
    params: { path: { pageID: plans.id } },
    body: { view: [{ type: "user", id: alice.id }], edit: [] },
  });
  expect(saved.response.status).toBe(200);
  return { salaries: salaries.id };
}

async function openSalaries(page: Page, key: string, id: string): Promise<void> {
  await expect(async () => {
    await page.goto(`/s/${key}/p/${id}/salaries`);
    await expect(page.locator("[data-page-title]")).toHaveText("Salaries", { timeout: 1_000 });
  }).toPass();
}

/** Asks about bob and waits for the answer. */
async function inspectBob(dialog: Locator): Promise<Locator> {
  await dialog.getByRole("combobox", { name: "Person to check" }).fill("bob");
  await dialog.locator('[data-subject-option="Bob Builder"]').click();
  const report = dialog.locator("[data-access-report]");
  await expect(report.locator("[data-access-right]")).toHaveCount(4);
  return report;
}

async function openInspector(page: Page): Promise<Locator> {
  await page.locator('[data-action="page-menu"]').click();
  await page.locator('[role="menu"] [data-action="inspect-access"]').click();
  const dialog = page.locator("[data-access-dialog]");
  await expect(dialog).toBeVisible();
  return dialog;
}

test.describe("checking access", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an administrator sees which restriction keeps bob out of a page", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const { salaries } = await restrictedPage(api, key, uniqueName(testInfo, "Inspect"));
    await openSalaries(page, key, salaries);

    const dialog = await openInspector(page);
    const report = await inspectBob(dialog);
    await expect(report.getByRole("heading", { name: "What Bob Builder may do" })).toBeVisible();
    const view = report.locator('[data-access-right="view"]');
    await expect(view).toHaveAttribute("data-allowed", "false");
    const decides = view.locator("[data-decides]");
    await expect(decides).toContainText("Not on the view list of Plans.");
    await expect(decides).toContainText("This decides it");
    await expect(decides.locator('[data-subject="Alice Admin"]')).toBeVisible();
    await expect(view.locator('[data-access-step="space"][data-passed="true"]')).toContainText("Holds the View permission in this space.");
    await expect(report.locator('[data-access-right="edit"] [data-decides]')).toContainText("May not view the page");
    await expectAccessible(page);
  });

  test("bob may not check anybody's access", async ({ api, apiAs, pageAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Refused"));
    const notes = await createPage(api, space.homePageId, "Notes");
    const alice = (await api.GET("/auth/me")).data!.user;

    const bobApi = await apiAs("bob");
    await expect(async () => {
      const refused = await bobApi.GET("/pages/{pageID}/access/{userID}", { params: { path: { pageID: notes.id, userID: alice.id } } });
      expect(refused.response.status).toBe(403);
      expect((refused.error as { error?: { message?: string } } | undefined)?.error?.message).toMatch(
        /^Only an administrator of this space can check what somebody may do here\. Ask one of them/,
      );
    }).toPass();

    const bob = await pageAs("bob");
    await expect(async () => {
      await bob.goto(`/s/${key}/p/${notes.id}/notes`);
      await expect(bob.locator("[data-page-title]")).toHaveText("Notes", { timeout: 1_000 });
    }).toPass();
    await bob.locator('[data-action="page-menu"]').click();
    await expect(bob.locator('[role="menu"] [data-action="copy-page"]')).toBeVisible();
    await expect(bob.locator('[role="menu"] [data-action="inspect-access"]')).toHaveCount(0);
  });

  test("the inspector works from the keyboard", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const { salaries } = await restrictedPage(api, key, uniqueName(testInfo, "Keys"));
    await openSalaries(page, key, salaries);

    const trigger = page.locator('[data-action="page-menu"]');
    await trigger.focus();
    await page.keyboard.press("Enter");
    const item = page.locator('[role="menu"] [data-action="inspect-access"]');
    await expect(page.getByRole("menu")).toBeVisible();
    await expect(async () => {
      if (!(await item.evaluate((el) => el === document.activeElement))) await page.keyboard.press("ArrowDown");
      await expect(item).toBeFocused({ timeout: 200 });
    }).toPass();
    await page.keyboard.press("Enter");

    const dialog = page.locator("[data-access-dialog]");
    const box = dialog.getByRole("combobox", { name: "Person to check" });
    await expect(box).toBeFocused();
    await page.keyboard.type("bob");
    await page.keyboard.press("Enter");
    await expect(dialog.locator('[data-access-right="view"] [data-decides]')).toContainText("Not on the view list of Plans.");

    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the inspector passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const key = uniqueKey(testInfo);
      made.push(key);
      const { salaries } = await restrictedPage(api, key, uniqueName(testInfo, `Axe ${scheme}`));
      await startInScheme(page, scheme);
      await openSalaries(page, key, salaries);
      const dialog = await openInspector(page);
      await expectAccessible(page);
      await inspectBob(dialog);
      await expectAccessible(page);
    });
  }
});
