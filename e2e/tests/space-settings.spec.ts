import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { uniqueName } from "../fixtures/seed";
import { confirm, expectAccessible, scrollsSideways, toast } from "../fixtures/shell";
import { createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const SECTIONS = [
  { section: "general", tab: "General", shows: "[data-space-details]" },
  { section: "workflow", tab: "Workflow", shows: "[data-space-review-rule]" },
  { section: "templates", tab: "Templates", shows: '[data-template-list="space"]' },
  { section: "members", tab: "Members and permissions", shows: "[data-space-permissions]" },
  { section: "integrations", tab: "Integrations", shows: "[data-shortcut-form]" },
  { section: "export", tab: "Import and export", shows: "[data-space-export]" },
  { section: "trash", tab: "Trash and archive", shows: "[data-space-archive]" },
] as const;

const READ_ONLY = "Only the space's administrators change its settings. Ask one of them, or ask for the role.";

const tab = (page: Page, name: string) => page.getByRole("tablist", { name: "Space settings" }).getByRole("tab", { name, exact: true });

test.describe("a space's settings", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("alice reaches every section by its address, with its tab chosen and the space then Settings above it", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Settings"));

    for (const { section, tab: name, shows } of SECTIONS) {
      await page.goto(`/spaces/${key}/settings/${section}`);
      await expect(tab(page, name)).toHaveAttribute("aria-selected", "true");
      await expect(page.locator(`[data-settings-section="${section}"] ${shows}`).first()).toBeVisible();
      await expect(page.locator("[data-space-settings]")).toHaveAttribute("data-page-width", "content");
      await expect(page.locator('[data-crumb="space"]')).toHaveText(space.name);
      await expect(page.locator('[data-crumb="space-settings"]')).toHaveCount(section === "general" ? 0 : 1);
    }
    await expect(page.getByRole("tablist", { name: "Space settings" }).getByRole("tab")).toHaveText(SECTIONS.map((each) => each.tab));

    await tab(page, "General").click();
    await expect(page).toHaveURL(new RegExp(`/spaces/${key}/settings/general$`));
    await page.goBack();
    await expect(page).toHaveURL(new RegExp(`/spaces/${key}/settings/trash$`));
    await page.reload();
    await expect(tab(page, "Trash and archive")).toHaveAttribute("aria-selected", "true");
    await expectAccessible(page);
    expect(await scrollsSideways(page)).toBe(false);
  });

  test("alice saves the details with a toast and deletes the space after a dialog asks", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    await createSpace(api, key, uniqueName(testInfo, "Doomed"));

    await page.goto(`/spaces/${key}/settings/general`);
    const renamed = uniqueName(testInfo, "Renamed");
    await page.getByLabel("Name", { exact: true }).fill(renamed);
    await page.locator('[data-action="save-space"]').click();
    await expect(toast(page, "Saved.")).toBeVisible();

    await page.locator('[data-action="delete-space"]').click();
    await expect(page.getByRole("dialog", { name: `Delete space ${renamed}?` })).toBeVisible();
    await confirm(page);
    await expect(page).toHaveURL(/\/spaces$/);
    made.splice(made.indexOf(key), 1);
  });

  test("bob opens the settings from the sidebar, read-only, and is told who to ask where he may not go", async ({ api, pageAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    await createSpace(api, key, uniqueName(testInfo, "Shared"));
    const bob = await pageAs("bob");

    await bob.goto(`/spaces/${key}`);
    await bob.locator('[data-nav="space-settings"]').click();
    await expect(bob).toHaveURL(new RegExp(`/spaces/${key}/settings/general$`));
    await expect(bob.locator("[data-settings-read-only]")).toHaveText(READ_ONLY);
    await expect(bob.getByRole("tablist", { name: "Space settings" }).getByRole("tab")).toHaveText([
      "General",
      "Templates",
      "Integrations",
      "Trash and archive",
    ]);
    await expect(bob.locator('[data-action="save-space"]')).toHaveCount(0);

    for (const section of ["workflow", "members", "export"]) {
      await bob.goto(`/spaces/${key}/settings/${section}`);
      await expect(bob.locator(`[data-settings-refused="${section}"]`)).toContainText("Only the space's administrators change its settings");
      await expect(bob.locator(`[data-settings-refused="${section}"]`)).toContainText("Ask one of them, or ask for the role.");
    }
    await expectAccessible(bob);
  });

  test("an address from before spaces moved still leads where it did", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Moved"));

    await page.goto(`/s/${key.toLowerCase()}`);
    await expect(page).toHaveURL(new RegExp(`/spaces/${key}$`));
    await page.goto(`/s/${key}/p/${space.homePageId}`);
    await expect(page).toHaveURL(new RegExp(`/spaces/${key}$`));
    await page.goto(`/s/${key}/settings?tab=shortcuts`);
    await expect(page).toHaveURL(new RegExp(`/spaces/${key}/settings/integrations$`));
    await page.goto(`/s/${key}/settings?tab=archive`);
    await expect(page).toHaveURL(new RegExp(`/spaces/${key}/settings/trash#archive$`));
    await page.goto(`/s/${key}/blog?year=2026`);
    await expect(page).toHaveURL(new RegExp(`/spaces/${key}/blog\\?year=2026$`));
  });
});
