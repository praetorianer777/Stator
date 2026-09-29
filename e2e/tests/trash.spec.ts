import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { childTitles, createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const trashRow = (page: Page, title: string) => page.locator(`[data-trash-item="${title}"]`);

test.describe("the trash", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("a deleted page is restored from the trash to where it was", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Trash"));
    const plans = await createPage(api, space.homePageId, "Plans");
    const old = await createPage(api, plans.id, "Old plans");
    await createPage(api, old.id, "Older plans");

    // The page asks before it goes; a person accepts.
    page.on("dialog", (dialog) => void dialog.accept());
    await page.goto(`/s/${key}/p/${old.id}/old-plans`);
    await page.locator('[data-action="page-menu"]').click();
    await page.locator('[role="menu"] [data-action="trash-page"]').click();
    await expect(page).toHaveURL(new RegExp(`/p/${plans.id}/plans$`));
    await expect(heading(page)).toHaveText("Plans");
    expect(await childTitles(api, key, plans.id)).toEqual([]);

    await page.goto(`/s/${key}/settings?tab=trash`);
    await expect(trashRow(page, "Old plans")).toContainText("2 pages");
    await expect(trashRow(page, "Old plans")).toContainText("Under Plans");
    await expectAccessible(page);
    await trashRow(page, "Old plans").locator('[data-action="restore-page"]').click();
    await expect(page.locator("[data-trash-notice]")).toHaveText("Restored Old plans.");
    await expect(trashRow(page, "Old plans")).toHaveCount(0);
    expect(await childTitles(api, key, plans.id)).toEqual(["Old plans"]);
    expect(await childTitles(api, key, old.id)).toEqual(["Older plans"]);
  });

  test("an administrator deletes a page for good", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Purge"));
    const doomed = await createPage(api, space.homePageId, "Doomed");
    await api.DELETE("/pages/{pageID}", { params: { path: { pageID: doomed.id } } });

    page.on("dialog", (dialog) => void dialog.accept());
    await page.goto(`/s/${key}/settings?tab=trash`);
    await trashRow(page, "Doomed").locator('[data-action="purge-page"]').click();
    await expect(page.locator("[data-trash-notice]")).toHaveText("Deleted Doomed for good.");
    await expect(page.getByText("The trash is empty.")).toBeVisible();
    const { response } = await api.GET("/pages/{pageID}", { params: { path: { pageID: doomed.id } } });
    expect(response.status).toBe(404);
  });
});
