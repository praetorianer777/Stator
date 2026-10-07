import type { Locator, Page } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });

/** Picks somebody from a people and groups picker by what their name starts with. */
async function pick(scope: Locator, typed: string, name: string): Promise<void> {
  await scope.getByRole("combobox").fill(typed);
  await scope.locator(`[data-subject-option="${name}"]`).click();
}

async function openRestrictions(page: Page): Promise<Locator> {
  await page.locator('[data-action="page-menu"]').click();
  await page.locator('[role="menu"] [data-action="page-restrictions"]').click();
  const dialog = page.locator("[data-restrictions-dialog]");
  await expect(dialog.locator("[data-restrictions]")).toBeVisible();
  return dialog;
}

/** Makes a space everybody may only view, which alice administers, with two pages side by side. */
async function readOnlySpace(api: StatorApi, key: string, name: string) {
  const space = await createSpace(api, key, name);
  const guide = await createPage(api, space.homePageId, "Guide");
  const sibling = await createPage(api, space.homePageId, "Sibling");
  const alice = must(await api.GET("/auth/me")).user;
  must(
    await api.PUT("/spaces/{spaceKey}/permissions", {
      params: { path: { spaceKey: key } },
      body: {
        grants: [
          { subject: { type: "everyone" }, permissions: ["view", "addComments"] },
          { subject: { type: "user", id: alice.id }, permissions: ["administer"] },
        ],
      },
    }),
  );
  return { space, guide, sibling };
}

/** Whether bob may edit a page, read through the API, which a replica may lag behind on. */
async function bobMayEdit(bobApi: StatorApi, id: string): Promise<boolean | undefined> {
  return (await bobApi.GET("/pages/{pageID}", { params: { path: { pageID: id } } })).data?.page.can.edit;
}

test.describe("letting somebody edit one page", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an administrator lets bob, who may only view the space, edit one page", async ({ page, api, apiAs, pageAs }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    made.push(key);
    const { guide, sibling } = await readOnlySpace(api, key, uniqueName(testInfo, "Grants"));
    const bobApi = await apiAs("bob");
    await expect.poll(() => bobMayEdit(bobApi, guide.id)).toBe(false);

    // On the edit list alone bob still cannot edit, and the dialog says why and what to do.
    await page.goto(`/s/${key}/p/${guide.id}/guide`);
    let dialog = await openRestrictions(page);
    const edit = dialog.locator('[data-restriction-list="edit"]');
    await pick(edit, "bob", "Bob Builder");
    await expect(edit.locator('[data-subject="Bob Builder"]')).toHaveAttribute("data-subject-warning", "Cannot edit");
    await expect(edit.locator("[data-cannot-edit-note]")).toContainText("Add them to Also allowed to edit below, or give them Add pages");
    await dialog.locator('[data-action="save-restrictions"]').click();
    await expect(dialog).toHaveCount(0);

    dialog = await openRestrictions(page);
    await expect(dialog.locator('[data-restriction-list="edit"] [data-subject="Bob Builder"]')).toHaveAttribute("data-subject-warning", "Cannot edit");
    const grant = dialog.locator('[data-restriction-list="editGrant"]');
    await expect(grant).toContainText("Nobody else.");
    await pick(grant, "bob", "Bob Builder");
    await expect(dialog.locator("[data-subject-warning]")).toHaveCount(0);
    await dialog.locator('[data-action="save-restrictions"]').click();
    await expect(dialog).toHaveCount(0);
    await expect.poll(() => bobMayEdit(bobApi, guide.id)).toBe(true);

    const bob = await pageAs("bob");
    await openUntil(bob, `/s/${key}/p/${guide.id}/guide`, () => expect(bob.locator('[data-action="edit-page"]')).toBeVisible(ONE_LOOK));
    // Editing the page is all the grant gives: nothing below it is added from here.
    await expect(bob.locator('[data-action="new-page"]')).toHaveCount(0);
    await bob.locator('[data-action="edit-page"]').click();
    await caretTo(bob.locator("#page-body"), "end");
    await bob.keyboard.type(" Kept up by bob.");
    await expect(bob.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(bob, "Bob's first edit.");
    await expect(bob.locator("main")).toContainText("Kept up by bob.");
    await openUntil(page, `/s/${key}/p/${guide.id}/guide`, () => expect(page.locator("main")).toContainText("Kept up by bob.", ONE_LOOK));

    await openUntil(bob, `/s/${key}/p/${sibling.id}/sibling`, () => expect(heading(bob)).toHaveText("Sibling", ONE_LOOK));
    await expect(bob.locator('[data-action="edit-page"]')).toHaveCount(0);
    const refused = await bobApi.PUT("/pages/{pageID}/draft", {
      params: { path: { pageID: sibling.id } },
      body: { title: "Sibling", body: { type: "doc", content: [{ type: "paragraph" }] }, baseVersion: 1 },
    });
    expect(refused.response.status).toBe(403);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the restrictions dialog with who else may edit passes axe in ${scheme}`, async ({ page, api, apiAs }, testInfo) => {
      const key = uniqueKey(testInfo);
      made.push(key);
      const { guide } = await readOnlySpace(api, key, uniqueName(testInfo, `Grant axe ${scheme}`));
      const bob = must(await (await apiAs("bob")).GET("/auth/me")).user;
      must(
        await api.PUT("/pages/{pageID}/restrictions", {
          params: { path: { pageID: guide.id } },
          body: { view: [], edit: [{ type: "user", id: bob.id }], editGrant: [] },
        }),
      );
      await startInScheme(page, scheme);
      await page.goto(`/s/${key}/p/${guide.id}/guide`);
      const dialog = await openRestrictions(page);
      await expect(dialog.locator('[data-restriction-list="edit"] [data-cannot-edit-note]')).toBeVisible();
      await expect(dialog.locator('[data-restriction-list="editGrant"] [role="combobox"]')).toBeVisible();
      await expectAccessible(page);
    });
  }
});
