import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const dialog = (page: Page) => page.locator("[data-new-page-dialog]");
const option = (page: Page, name: string) => dialog(page).getByRole("radio", { name: new RegExp(`^${name}`) });
const preview = (page: Page) => dialog(page).locator("[data-template-preview]");
const body = (page: Page) => page.locator("#page-body");
const draftStatus = (page: Page) => page.locator("[data-draft-status]");

/** Opens the new page dialog under the space's home page and waits for the templates. */
async function openNewPage(page: Page, space: Space) {
  await page.goto(`/s/${space.key}`);
  await page.locator('[data-action="new-page"]').click();
  await expect(option(page, "Project plan")).toBeVisible();
}

test.describe("page templates", { tag: ["@auth", "@desktop"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("a page made from a template loses a hint to typing, and publishes without the rest", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Template");
    await openNewPage(page, space);
    await option(page, "Decision record").click();
    await expect(option(page, "Decision record")).toHaveAttribute("aria-checked", "true");
    await expect(preview(page).getByRole("heading", { name: "Options considered" })).toBeVisible();
    await dialog(page).getByLabel("Title", { exact: true }).fill("Pick a database");
    await dialog(page).locator('[data-action="confirm-new-page"]').click();
    await expect(page).toHaveURL(/\/p\/[0-9a-f-]+\/pick-a-database\/edit$/);
    const id = /\/p\/([0-9a-f-]+)\//.exec(page.url())![1]!;

    const hint = body(page).locator("[data-hint]", { hasText: "State the option chosen and why it won." });
    await expect(hint).toBeVisible();
    const hints = await body(page).locator("[data-hint]").count();
    await hint.click();
    await page.keyboard.type("We chose Postgres.");
    await expect(body(page)).toContainText("We chose Postgres.");
    await expect(hint).toHaveCount(0);
    await expect(body(page).locator("[data-hint]")).toHaveCount(hints - 1);
    await expect(draftStatus(page)).toHaveAttribute("data-draft-status", "saved");

    await publishFromEditor(page);
    const doc = page.locator("[data-doc]");
    await expect(doc).toContainText("We chose Postgres.");
    await expect(doc.getByRole("heading", { name: "Options considered" })).toBeVisible();
    await expect(doc.locator("[data-hint]")).toHaveCount(0);
    await expect(doc).not.toContainText("Describe another option");
    const published = must(await api.GET("/pages/{pageID}", { params: { path: { pageID: id } } })).page;
    expect(published.unpublished).toBe(false);
    expect(JSON.stringify(published.body)).not.toContain('"hint"');
  });

  test("the picker is worked from the keyboard alone", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Keys");
    await openNewPage(page, space);
    await page.keyboard.press("Tab");
    await expect(option(page, "Blank page")).toBeFocused();
    await page.keyboard.press("ArrowDown");
    await expect(option(page, "Meeting notes")).toBeFocused();
    await expect(option(page, "Meeting notes")).toHaveAttribute("aria-checked", "true");
    await expect(dialog(page).getByLabel("Title", { exact: true })).toHaveValue(/^Meeting notes \d{4}-\d{2}-\d{2}$/);
    await expect(preview(page).getByRole("heading", { name: "Action items" })).toBeVisible();
    await page.keyboard.press("Shift+Tab");
    await expect(dialog(page).getByLabel("Title", { exact: true })).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/p\/[0-9a-f-]+\/meeting-notes-[0-9-]+\/edit$/);
    await expect(body(page).locator("[data-hint]").first()).toBeVisible();
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the picker and a page with hints pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      await startInScheme(page, scheme);
      await openNewPage(page, space);
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expectAccessible(page);
      await option(page, "Product requirements").click();
      await expect(preview(page).getByRole("table").first()).toBeVisible();
      await expectAccessible(page);
      await option(page, "How-to guide").click();
      await dialog(page).getByLabel("Title", { exact: true }).fill("Rotate the keys");
      await dialog(page).locator('[data-action="confirm-new-page"]').click();
      await expect(body(page).locator("[data-panel] [data-hint]")).toBeVisible();
      await expectAccessible(page);
    });
  }
});
