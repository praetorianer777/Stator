import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

// The title alone; the heading also carries the archived mark.
const heading = (page: Page) => page.locator("main [data-page-title]");
const banner = (page: Page) => page.locator("main [data-archived-banner]");
const archiveRow = (page: Page, title: string) => page.locator(`[data-archive-item="${title}"]`);

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** Opens a page until it shows what a test waits for, which a replica may lag behind. */
async function openShowing(page: Page, key: string, target: WikiPage, shown: (page: Page) => ReturnType<Page["locator"]>) {
  await expect(async () => {
    await page.goto(`/s/${key}/p/${target.id}/page`);
    await expect(heading(page)).toHaveText(target.title, { timeout: 1_000 });
    await expect(shown(page)).toBeVisible({ timeout: 1_000 });
  }).toPass();
}

test.describe("archived pages and spaces", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshTree(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, label));
    const plans = await createPage(api, space.homePageId, uniqueName(testInfo, "Plans"), doc("Plans of a past year."));
    const old = await createPage(api, plans.id, uniqueName(testInfo, "Roadmap"), doc("Mothballed words."));
    return { key, space, plans, old };
  }

  test("alice archives a page with the page below it, bob reads both and changes neither, finds them only when he asks, and alice unarchives them", async ({
    page,
    api,
    apiAs,
    pageAs,
  }, testInfo) => {
    const { key, plans, old } = await freshTree(api, testInfo, "Archive");

    // The page asks before it archives; a person accepts.
    page.on("dialog", (dialog) => void dialog.accept());
    await page.goto(`/s/${key}/p/${plans.id}/page`);
    await expect(heading(page)).toHaveText(plans.title);
    await page.locator('[data-action="page-menu"]').click();
    await page.locator('[role="menu"] [data-action="archive-page"]').click();
    await expect(banner(page)).toHaveAttribute("data-archived-banner", "page");
    await expect(banner(page)).toContainText("This page is archived.");
    await expect(page.locator('[data-action="edit-page"]')).toHaveCount(0);
    expect(await scrollsSideways(page)).toBe(false);

    const bob = await pageAs("bob");
    await openShowing(bob, key, old, banner);
    await expect(banner(bob)).toHaveAttribute("data-archived-banner", "with");
    await expect(banner(bob).getByRole("link", { name: `Go to ${plans.title}` })).toBeVisible();
    await expect(bob.locator('[data-action="edit-page"]')).toHaveCount(0);
    await expect(bob.locator("main [data-archived-mark]")).toHaveText("Archived");
    expect(await scrollsSideways(bob)).toBe(false);

    const bobApi = await apiAs("bob");
    const refused = await bobApi.PATCH("/pages/{pageID}", { params: { path: { pageID: old.id } }, body: { title: "Changed", version: 1 } });
    expect(refused.response.status).toBe(409);
    expect((refused.error as { error: { code: string; message: string } }).error).toMatchObject({
      code: "archived",
      message: "This page is archived, so nothing on it changes. Ask an administrator of the space to unarchive it first.",
    });

    const hit = bob.locator(`[data-search-hit="${old.title}"]`);
    await expect(async () => {
      await bob.goto(`/search?q=${encodeURIComponent("Mothballed")}&space=${key}`);
      await expect(bob.getByText("Nothing matches")).toBeVisible({ timeout: 1_000 });
    }).toPass();
    await bob.getByRole("checkbox", { name: "Include archived pages" }).check();
    await expect(hit.locator("[data-archived-mark]")).toBeVisible();
    await expect(bob).toHaveURL(/archived=true/);

    await page.goto(`/s/${key}/settings?tab=archive`);
    await expect(archiveRow(page, plans.title)).toContainText("2 pages");
    await expectAccessible(page);
    await archiveRow(page, plans.title)
      .getByRole("button", { name: `Unarchive ${plans.title}` })
      .click();
    await expect(page.locator("[data-archive-notice]")).toHaveText(`Unarchived ${plans.title}.`);
    await expect(page.getByText("Nothing is archived in this space.")).toBeVisible();

    await page.goto(`/s/${key}/p/${old.id}/page`);
    await expect(heading(page)).toHaveText(old.title);
    await expect(banner(page)).toHaveCount(0);
    await expect(page.locator('[data-action="edit-page"]')).toBeVisible();
  });

  test("alice archives a whole space, which leaves the list of spaces until asked for and comes back whole", async ({ page, api }, testInfo) => {
    const { key, space, plans } = await freshTree(api, testInfo, "Shelved");

    page.on("dialog", (dialog) => void dialog.accept());
    await page.goto(`/s/${key}/settings`);
    await page.locator('[data-action="archive-space"]').click();
    await expect(page.locator("[data-space-archive-card]")).toHaveAttribute("data-space-archive-card", "archived");
    await expect(page.locator("[data-space-archive-card]")).toContainText("archived this space");

    await page.goto("/spaces");
    const row = page.locator(`[data-space-row="${key}"]`);
    await expect(page.getByRole("switch", { name: "Show archived spaces" })).toBeVisible();
    await expect(row).toHaveCount(0);
    await page.getByRole("switch", { name: "Show archived spaces" }).click();
    await expect(row.locator("[data-archived-mark]")).toBeVisible();
    await expect(row).toContainText(space.name);
    expect(await scrollsSideways(page)).toBe(false);

    await page.goto(`/s/${key}/p/${plans.id}/page`);
    await expect(banner(page)).toHaveAttribute("data-archived-banner", "space");
    await expect(banner(page)).toContainText("This whole space is archived.");
    await expect(page.locator('[data-action="edit-page"]')).toHaveCount(0);
    await banner(page).getByRole("link", { name: "Open the space settings" }).click();
    await page.locator('[data-action="unarchive-space"]').click();
    await expect(page.locator("[data-space-archive-card]")).toHaveAttribute("data-space-archive-card", "live");

    await page.goto(`/s/${key}/p/${plans.id}/page`);
    await expect(heading(page)).toHaveText(plans.title);
    await expect(banner(page)).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`archiving and unarchiving work from the keyboard and are accessible in ${scheme}`, async ({ page, api }, testInfo) => {
      const { key, plans } = await freshTree(api, testInfo, `Axe ${scheme}`);
      await startInScheme(page, scheme);
      page.on("dialog", (dialog) => void dialog.accept());

      await page.goto(`/s/${key}/p/${plans.id}/page`);
      await expect(heading(page)).toHaveText(plans.title);
      await page.locator('[data-action="page-menu"]').focus();
      await page.keyboard.press("Enter");
      await page.getByRole("menuitem", { name: "Archive" }).focus();
      await page.keyboard.press("Enter");
      await expect(banner(page)).toHaveAttribute("data-archived-banner", "page");
      await expectAccessible(page);

      const unarchive = banner(page).getByRole("button", { name: "Unarchive" });
      await unarchive.focus();
      await page.keyboard.press("Enter");
      await expect(banner(page)).toHaveCount(0);
      await expect(page.locator('[data-action="edit-page"]')).toBeVisible();

      must(await api.PUT("/pages/{pageID}/archive", { params: { path: { pageID: plans.id } } }));
      await page.goto(`/s/${key}/settings?tab=archive`);
      await expect(archiveRow(page, plans.title)).toBeVisible();
      await expectAccessible(page);
      await archiveRow(page, plans.title)
        .getByRole("button", { name: `Unarchive ${plans.title}` })
        .focus();
      await page.keyboard.press("Enter");
      await expect(page.getByText("Nothing is archived in this space.")).toBeVisible();
    });
  }
});
