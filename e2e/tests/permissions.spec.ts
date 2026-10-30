import type { Locator, Page } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { ONE_LOOK, openShowing, openUntil, openWithout } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { everyoneGroup } from "../fixtures/grants";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const NOT_FOUND = /not found/i;
// The spec's own people, named after it (fixtures/team.ts).
let ALICE = "";
let BOB = "";

/** Picks somebody from a people and groups picker by what their name starts with. */
async function pick(scope: Locator, typed: string, name: string): Promise<void> {
  await scope.getByRole("combobox").fill(typed);
  await scope.locator(`[data-subject-option="${name}"]`).click();
}

/** What bob gets for a page, read through the API, which a replica may lag behind on. */
async function statusOf(api: StatorApi, id: string): Promise<number> {
  return (await api.GET("/pages/{pageID}", { params: { path: { pageID: id } } })).response.status;
}

/** Checks the tree marks a page restricted; on a narrow screen the tree is in the drawer, which is opened and closed again. */
async function expectTreeLock(page: Page, id: string): Promise<void> {
  const drawer = page.locator('[data-action="drawer"]');
  const narrow = await drawer.isVisible();
  if (narrow) await drawer.click();
  await expect(page.locator(`[data-tree-item="${id}"] [data-tree-restricted]`)).toBeVisible();
  if (narrow) await page.locator('[data-action="close-drawer"]').click();
}

async function openRestrictions(page: Page): Promise<Locator> {
  await page.locator('[data-action="page-menu"]').click();
  await page.locator('[role="menu"] [data-action="page-restrictions"]').click();
  const dialog = page.locator("[data-restrictions-dialog]");
  await expect(dialog.locator("[data-restrictions]")).toBeVisible();
  return dialog;
}

test.describe("permissions", { tag: ["@auth"] }, () => {
  test.beforeEach(({ team }) => {
    ALICE = team.people.alice.name;
    BOB = team.people.bob.name;
  });
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an administrator takes a space from Everyone and grants it to bob", async ({ page, api, apiAs, pageAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Closed"));
    const bobApi = await apiAs("bob");
    await expect.poll(() => statusOf(bobApi, space.homePageId)).toBe(200);

    await page.goto(`/s/${key}/settings?tab=permissions`);
    const grants = page.getByRole("table", { name: "Roles granted in this space" });
    const everyone = grants.locator('[data-grant="Everyone"][data-grant-role="editor"]');
    await expect(everyone).toBeVisible();
    await expectAccessible(page);
    await everyone.locator('[data-action="revoke-grant"]').click();
    await page.locator("[data-confirm]").locator('[data-action="confirm"]').click();
    await expect(page.getByText("Everyone no longer holds Editor here.", { exact: true })).toBeVisible();
    await expect.poll(() => statusOf(bobApi, space.homePageId)).toBe(404);

    const bob = await pageAs("bob");
    await openWithout(bob, "/spaces", bob.locator(`[data-space-row="${key}"]`), bob.locator("[data-space-directory]"));

    const form = page.locator("[data-grant-form]");
    await pick(form.locator("[data-subject-picker]"), BOB, BOB);
    await form.locator("[data-grant-role-choice]").selectOption("viewer");
    await form.locator('[data-action="grant-role"]').click();
    await expect(page.getByText(`${BOB} holds Viewer here now.`, { exact: true })).toBeVisible();
    await expect(grants.locator(`[data-grant="${BOB}"][data-grant-role="viewer"]`)).toBeVisible();
    await expect.poll(() => statusOf(bobApi, space.homePageId)).toBe(200);
    await openUntil(bob, `/s/${key}`, () => expect(bob.locator("[data-page-home]")).toBeAttached(ONE_LOOK));
    // A viewer views and nothing more, so nothing on the page offers to change it.
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
    await pick(view, ALICE, ALICE);
    await dialog.locator('[data-action="save-restrictions"]').click();
    await expect(dialog).toHaveCount(0);
    await expect(page.locator('[data-page-restricted="view"]')).toBeVisible();
    await expectTreeLock(page, secret.id);
    await expect.poll(() => statusOf(bobApi, secret.id)).toBe(404);

    const bob = await pageAs("bob");
    await openWithout(bob, `/s/${key}/p/${secret.id}/salaries`, bob.locator("[data-page-title]"), bob.getByText(NOT_FOUND).first());
    await openUntil(bob, `/s/${key}`, async () => {
      await expect(bob.locator("[data-page-home]")).toBeAttached(ONE_LOOK);
      await expect(bob.locator(`[data-tree-item="${secret.id}"]`)).toHaveCount(0, ONE_LOOK);
    });

    dialog = await openRestrictions(page);
    await pick(dialog.locator('[data-restriction-list="view"]'), BOB, BOB);
    await dialog.locator('[data-action="save-restrictions"]').click();
    await expect(dialog).toHaveCount(0);
    await expect.poll(() => statusOf(bobApi, secret.id)).toBe(200);
    await openUntil(bob, `/s/${key}/p/${secret.id}/salaries`, () => expect(heading(bob)).toHaveText("Salaries", ONE_LOOK));
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
    await openUntil(bob, `/s/${key}/p/${notes.id}/notes`, () => expect(heading(bob)).toHaveText("Notes", ONE_LOOK));
    const dialog = await openRestrictions(bob);
    await pick(dialog.locator('[data-restriction-list="edit"]'), ALICE, ALICE);
    await dialog.locator('[data-action="save-restrictions"]').click();
    await expect(dialog.getByText(/Saving this would shut you out of the page\. Add yourself, or a group you are in, to both lists/)).toBeVisible();
    await expect(dialog).toBeVisible();

    await dialog.locator('[data-action="add-me-edit"]').click();
    await dialog.locator('[data-action="save-restrictions"]').click();
    await expect(dialog).toHaveCount(0);
    await expect(bob.locator('[data-page-restricted="edit"]')).toBeVisible();
  });

  test("a member no role lets use Stator is told why", async ({ page, api, pageAs }) => {
    const everyone = await everyoneGroup(api);
    try {
      await page.goto("/admin/access/roles");
      const use = page.locator(`[data-grant="Everyone"][data-grant-role="member"]`);
      await expect(use).toBeVisible();
      await expectAccessible(page);
      await use.locator('[data-action="revoke-grant"]').click();
      await page.locator("[data-confirm]").locator('[data-action="confirm"]').click();
      await expect(page.getByText("Everyone no longer holds Member.", { exact: true })).toBeVisible();

      const bob = await pageAs("bob");
      await openShowing(bob, "/spaces", bob.locator("[data-no-access]"));
      await expect(bob.getByRole("heading", { level: 1 })).toContainText("You do not have access to");
      await expect(bob.getByText(/Ask an administrator to grant you one under Access/)).toBeVisible();
      await expectAccessible(bob);
    } finally {
      must(await api.POST("/role-assignments", { body: { role: "member", spaceKey: "", userId: null, groupId: everyone } }));
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

      await page.goto("/admin/access/matrix");
      await expect(page.locator('[data-permission-cell="administrator:administerSite"]')).toBeDisabled();
      await expectAccessible(page);

      await page.goto(`/s/${key}/settings?tab=permissions`);
      await expect(page.getByRole("table", { name: "Roles granted in this space" })).toBeVisible();
      await page.locator("[data-space-permissions] [data-subject-picker] input").fill("b");
      await expect(page.locator("[data-space-permissions] [data-subject-options] [role=option]").first()).toBeVisible();
      await expectAccessible(page);

      await page.goto(`/s/${key}/p/${page2.id}/locked`);
      await expect(page.locator('[data-page-restricted="view"]')).toBeVisible();
      await expectTreeLock(page, page2.id);
      await expectAccessible(page);
      await openRestrictions(page);
      await expectAccessible(page);
    });
  }
});
