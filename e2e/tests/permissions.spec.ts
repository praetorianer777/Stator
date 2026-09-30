import type { Locator, Page } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const NOT_FOUND = /not found/i;

/** Picks somebody from a people and groups picker by what their name starts with. */
async function pick(scope: Locator, typed: string, name: string): Promise<void> {
  await scope.getByRole("combobox").fill(typed);
  await scope.locator(`[data-subject-option="${name}"]`).click();
}

/** Opens a page and waits for either its title or the sentence that it is not there. */
async function openPage(page: Page, path: string): Promise<void> {
  await page.goto(path);
  await expect(page.locator("[data-page-title], [role=alert], [data-route-error], [data-not-found]").first()).toBeVisible();
}

/** What bob gets for a page, read through the API, which a replica may lag behind on. */
async function statusOf(api: StatorApi, id: string): Promise<number> {
  return (await api.GET("/pages/{pageID}", { params: { path: { pageID: id } } })).response.status;
}

async function openRestrictions(page: Page): Promise<Locator> {
  await page.locator('[data-action="page-menu"]').click();
  await page.locator('[role="menu"] [data-action="page-restrictions"]').click();
  const dialog = page.locator("[data-restrictions-dialog]");
  await expect(dialog.locator("[data-restrictions]")).toBeVisible();
  return dialog;
}

test.describe("permissions", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an administrator takes a space from everyone and gives it to bob", async ({ page, api, apiAs, pageAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Closed"));
    const bobApi = await apiAs("bob");
    await expect.poll(() => statusOf(bobApi, space.homePageId)).toBe(200);

    await page.goto(`/s/${key}/settings?tab=permissions`);
    const grid = page.getByRole("table", { name: "Permissions in this space" });
    await expect(grid.locator('[data-grant-type="everyone"]')).toBeVisible();
    await expectAccessible(page);
    await grid.locator('[data-grant-type="everyone"] [data-action="remove-grant"]').click();
    await page.locator('[data-action="save-space-permissions"]').click();
    await expect(page.getByText("Saved.", { exact: true })).toBeVisible();
    await expect.poll(() => statusOf(bobApi, space.homePageId)).toBe(404);

    const bob = await pageAs("bob");
    await bob.goto("/spaces");
    await expect(bob.locator("[data-space-directory]")).toBeVisible();
    await expect(bob.locator(`[data-space-row="${key}"]`)).toHaveCount(0);

    await pick(page.locator("[data-space-permissions] [data-subject-picker]"), "bob", "Bob Builder");
    await expect(grid.locator('[data-grant="Bob Builder"] [data-permission-cell="view"]')).toBeChecked();
    await page.locator('[data-action="save-space-permissions"]').click();
    await expect(page.getByText("Saved.", { exact: true })).toBeVisible();
    await expect.poll(() => statusOf(bobApi, space.homePageId)).toBe(200);
    await expect(async () => {
      await bob.goto(`/s/${key}`);
      await expect(bob.locator("[data-page-home]")).toBeAttached({ timeout: 1_000 });
    }).toPass();
    // Viewing is all he was given, so nothing on the page offers to change it.
    await expect(bob.locator('[data-action="edit-page"]')).toHaveCount(0);
  });

  test("bob loses a page to its view list and gets it back", async ({ page, api, apiAs, pageAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Lists"));
    const secret = await createPage(api, space.homePageId, "Salaries");
    const bobApi = await apiAs("bob");
    await expect.poll(() => statusOf(bobApi, secret.id)).toBe(200);

    await page.goto(`/s/${key}/p/${secret.id}/salaries`);
    let dialog = await openRestrictions(page);
    const view = dialog.locator('[data-restriction-list="view"]');
    await expect(view).toContainText("Everyone who can view the space.");
    await pick(view, "alice", "Alice Admin");
    await dialog.locator('[data-action="save-restrictions"]').click();
    await expect(dialog).toHaveCount(0);
    await expect(page.locator('[data-page-restricted="view"]')).toBeVisible();
    await expect(page.locator(`[data-tree-item="${secret.id}"] [data-tree-restricted]`)).toBeVisible();
    await expect.poll(() => statusOf(bobApi, secret.id)).toBe(404);

    const bob = await pageAs("bob");
    await openPage(bob, `/s/${key}/p/${secret.id}/salaries`);
    await expect(bob.locator("[data-page-title]")).toHaveCount(0);
    await expect(bob.getByText(NOT_FOUND).first()).toBeVisible();
    await bob.goto(`/s/${key}`);
    await expect(bob.locator("[data-page-home]")).toBeAttached();
    await expect(bob.locator(`[data-tree-item="${secret.id}"]`)).toHaveCount(0);

    dialog = await openRestrictions(page);
    await pick(dialog.locator('[data-restriction-list="view"]'), "bob", "Bob Builder");
    await dialog.locator('[data-action="save-restrictions"]').click();
    await expect(dialog).toHaveCount(0);
    await expect.poll(() => statusOf(bobApi, secret.id)).toBe(200);
    await expect(async () => {
      await bob.goto(`/s/${key}/p/${secret.id}/salaries`);
      await expect(heading(bob)).toHaveText("Salaries", { timeout: 1_000 });
    }).toPass();
    await expect(bob.locator('[data-page-restricted="view"]')).toBeVisible();
  });

  test("a page below a restricted one shows what it inherits and from where", async ({ page, api, apiAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Inherit"));
    const plans = await createPage(api, space.homePageId, "Plans");
    const child = await createPage(api, plans.id, "Next year");
    const alice = (await api.GET("/auth/me")).data!.user;
    const saved = await api.PUT("/pages/{pageID}/restrictions", {
      params: { path: { pageID: plans.id } },
      body: { view: [{ type: "user", id: alice.id }], edit: [] },
    });
    expect(saved.response.status).toBe(200);

    await page.goto(`/s/${key}/p/${child.id}/next-year`);
    await expect(page.locator('[data-page-restricted="view"]')).toBeVisible();
    const dialog = await openRestrictions(page);
    const from = dialog.locator('[data-inherited-from="Plans"]');
    await expect(from).toContainText("From Plans");
    await expect(from).toContainText(alice.name);
    await expect(dialog.locator('[data-restriction-list="view"]')).toContainText("Everyone who can view the space.");
    await expectAccessible(page);

    const bobApi = await apiAs("bob");
    await expect.poll(() => statusOf(bobApi, child.id)).toBe(404);
  });

  test("a save that would shut the saver out is refused with what to do", async ({ api, pageAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Lockout"));
    const notes = await createPage(api, space.homePageId, "Notes");

    const bob = await pageAs("bob");
    await expect(async () => {
      await bob.goto(`/s/${key}/p/${notes.id}/notes`);
      await expect(heading(bob)).toHaveText("Notes", { timeout: 1_000 });
    }).toPass();
    const dialog = await openRestrictions(bob);
    await pick(dialog.locator('[data-restriction-list="edit"]'), "alice", "Alice Admin");
    await dialog.locator('[data-action="save-restrictions"]').click();
    await expect(dialog.getByText(/Saving this would shut you out of the page\. Add yourself, or a group you are in, to both lists/)).toBeVisible();
    await expect(dialog).toBeVisible();

    await dialog.locator('[data-action="add-me-edit"]').click();
    await dialog.locator('[data-action="save-restrictions"]').click();
    await expect(dialog).toHaveCount(0);
    await expect(bob.locator('[data-page-restricted="edit"]')).toBeVisible();
  });

  test("a member the organization does not let in is told why", async ({ page, api, pageAs }) => {
    const everyone = [{ type: "everyone" as const }];
    try {
      await page.goto("/settings/permissions");
      const use = page.locator('[data-global-permission="use"]');
      await expect(use.locator('[data-subject-type="everyone"]')).toBeVisible();
      await expectAccessible(page);
      await use.locator('[data-subject-type="everyone"] [data-action="remove-subject"]').click();
      await pick(use, "alice", "Alice Admin");
      await use.locator('[data-action="save-global"]').click();
      await expect(use.getByText("Saved.", { exact: true })).toBeVisible();

      const bob = await pageAs("bob");
      await expect(async () => {
        await bob.goto("/spaces");
        await expect(bob.locator("[data-no-access]")).toBeVisible({ timeout: 1_000 });
      }).toPass();
      await expect(bob.getByRole("heading", { level: 1 })).toContainText("You do not have access to");
      await expect(bob.getByText(/Ask one of its administrators to add you under Permissions/)).toBeVisible();
      await expectAccessible(bob);
    } finally {
      await api.PUT("/org/permissions/{permission}", { params: { path: { permission: "use" } }, body: { subjects: everyone } });
    }
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the permission screens pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const key = uniqueKey(testInfo);
      made.push(key);
      const space = await createSpace(api, key, uniqueName(testInfo, `Axe ${scheme}`));
      const page2 = await createPage(api, space.homePageId, "Locked");
      const alice = (await api.GET("/auth/me")).data!.user;
      await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: page2.id } }, body: { view: [{ type: "user", id: alice.id }], edit: [] } });
      await startInScheme(page, scheme);

      await page.goto("/settings/permissions");
      await expect(page.locator('[data-global-permission="administer"] [data-fixed]')).toBeVisible();
      await expectAccessible(page);

      await page.goto(`/s/${key}/settings?tab=permissions`);
      await expect(page.getByRole("table", { name: "Permissions in this space" })).toBeVisible();
      await page.locator("[data-space-permissions] [data-subject-picker] input").fill("b");
      await expect(page.locator("[data-space-permissions] [data-subject-options] [role=option]").first()).toBeVisible();
      await expectAccessible(page);

      await page.goto(`/s/${key}/p/${page2.id}/locked`);
      await expect(page.locator('[data-page-restricted="view"]')).toBeVisible();
      await expect(page.locator(`[data-tree-item="${page2.id}"] [data-tree-restricted]`)).toBeVisible();
      await expectAccessible(page);
      await openRestrictions(page);
      await expectAccessible(page);
    });
  }
});
