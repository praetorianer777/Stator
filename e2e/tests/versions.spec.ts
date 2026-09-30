import type { Page } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const draftStatus = (page: Page) => page.locator("[data-draft-status]");
const versionRow = (page: Page, n: number) => page.locator(`[data-version-row="${n}"]`);
const tree = (page: Page) => page.locator("[data-page-tree]");

const doc = (...lines: string[]) => ({ type: "doc", content: lines.map((text) => ({ type: "paragraph", content: [{ type: "text", text }] })) });

/** Publishes a new body as the next version through the API, the way the editor does: a draft, then publish. */
async function publishVersion(api: StatorApi, id: string, text: string, comment = ""): Promise<WikiPage> {
  const params = { path: { pageID: id } };
  const current = must(await api.GET("/pages/{pageID}", { params })).page;
  must(await api.PUT("/pages/{pageID}/draft", { params, body: { title: current.title, body: doc(text), baseVersion: current.version } }));
  return must(await api.POST("/pages/{pageID}/publish", { params, body: { comment } })).page;
}

/** Types at the end of the open editor's body and waits until the draft is saved. */
async function typeIntoDraft(page: Page, text: string) {
  await page.locator("#page-body").click();
  await page.keyboard.press("ControlOrMeta+End");
  await page.keyboard.type(text);
  await expect(draftStatus(page)).toHaveAttribute("data-draft-status", "saved");
}

test.describe("drafts, publishing and history", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: Parameters<typeof uniqueKey>[0], label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("an edit autosaves to a draft nobody else sees, and publishes with a comment", async ({ page, api, apiAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Draft");
    const plans = await createPage(api, space.homePageId, "Plans", doc("First words."));
    await page.goto(`/s/${space.key}/p/${plans.id}/plans/edit`);
    await typeIntoDraft(page, " More words.");

    const params = { params: { path: { pageID: plans.id } } };
    expect(JSON.stringify(must(await api.GET("/pages/{pageID}/draft", params)).draft?.body)).toContain("More words.");
    const bob = await apiAs("bob");
    expect(must(await bob.GET("/pages/{pageID}/draft", params)).draft).toBeNull();
    expect(JSON.stringify(must(await bob.GET("/pages/{pageID}", params)).page.body)).not.toContain("More words.");

    // The draft outlives the tab.
    await page.reload();
    await expect(page.locator("#page-body")).toContainText("More words.");
    await publishFromEditor(page, "Added more words.");
    await expect(page).toHaveURL(new RegExp(`/p/${plans.id}/plans$`));
    await expect(page.locator("[data-doc]")).toContainText("More words.");
    await expect(page.locator("[data-draft-note]")).toHaveCount(0);

    await page.locator('[data-action="page-history"]').click();
    await expect(versionRow(page, 2)).toContainText("Added more words.");
    await expect(versionRow(page, 2)).toContainText("Latest");
    await expect(versionRow(page, 1)).toContainText("No comment");
  });

  test("a new page is its creator's alone until it is published", { tag: "@desktop" }, async ({ page, api, apiAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "New");
    await page.goto(`/s/${space.key}`);
    await page.locator('[data-action="new-page"]').click();
    await page.getByRole("dialog").getByLabel("Title", { exact: true }).fill("Onboarding");
    await page.locator('[data-action="confirm-new-page"]').click();
    await expect(page).toHaveURL(/\/p\/[0-9a-f-]+\/onboarding\/edit$/);
    const id = /\/p\/([0-9a-f-]+)\//.exec(page.url())![1]!;
    await typeIntoDraft(page, "Start here.");

    await page.locator('[data-action="close-editor"]').click();
    await expect(page.locator("[data-unpublished]")).toHaveText("Unpublished");
    await expect(page.locator("[data-unpublished-note]")).toBeVisible();
    await expect(tree(page).getByRole("treeitem", { name: "Onboarding, unpublished", exact: true })).toBeVisible();
    const bob = await apiAs("bob");
    const params = { params: { path: { pageID: id } } };
    expect((await bob.GET("/pages/{pageID}", params)).response.status).toBe(404);

    await page.locator('[data-action="continue-draft"]').click();
    await publishFromEditor(page, "First version.");
    await expect(page.locator("[data-unpublished]")).toHaveCount(0);
    await expect(page.locator("[data-doc]")).toContainText("Start here.");
    await expect(tree(page).getByRole("treeitem", { name: "Onboarding", exact: true })).toBeVisible();
    await expect(async () => expect((await bob.GET("/pages/{pageID}", params)).response.status).toBe(200)).toPass();
  });

  test("a publish over somebody else's is refused until the draft is kept", async ({ page, api, pageAs }, testInfo) => {
    test.slow();
    const space = await freshSpace(api, testInfo, "Conflict");
    const plans = await createPage(api, space.homePageId, "Plans", doc("Shared words."));
    const editPath = `/s/${space.key}/p/${plans.id}/plans/edit`;
    await page.goto(editPath);
    await typeIntoDraft(page, " Alice was here.");

    const bob = await pageAs("bob");
    await expect(async () => {
      await bob.goto(editPath);
      await expect(bob.locator("#page-body")).toContainText("Shared words.", { timeout: 1_000 });
    }).toPass();
    await typeIntoDraft(bob, " Bob was here.");
    await publishFromEditor(bob, "Bob's change.");

    await page.locator('[data-action="publish"]').click();
    await page.locator('[data-publish-dialog] [data-action="confirm-publish"]').click();
    const conflict = page.locator("[data-publish-conflict]");
    await expect(conflict).toContainText("Somebody published this page while you were editing.");
    await expect(conflict).toContainText("Compare your draft with version 2 first.");
    await expectAccessible(page);

    await conflict.locator('[data-action="compare-conflict"]').click();
    await expect(page).toHaveURL(/\/history\?from=2&to=draft$/);
    await expect(page.locator("[data-compare-sides]")).toContainText("Your draft");
    await expect(page.locator("[data-diff-view] ins[data-diff]").filter({ hasText: "Alice" }).first()).toBeVisible();
    await expect(page.locator("[data-diff-view] del[data-diff]").filter({ hasText: "Bob" }).first()).toBeVisible();

    await page.locator('[data-action="continue-draft"]').click();
    await expect(page.locator("#page-body")).toContainText("Alice was here.");
    await page.locator('[data-action="publish"]').click();
    await page.locator('[data-publish-dialog] [data-action="confirm-publish"]').click();
    await conflict.locator('[data-action="keep-and-publish"]').click();
    await expect(page).toHaveURL(new RegExp(`/p/${plans.id}/plans$`));
    await expect(page.locator("[data-doc]")).toContainText("Alice was here.");
    await expect(page.locator("[data-doc]")).not.toContainText("Bob was here.");
    const { versions } = must(await api.GET("/pages/{pageID}/versions", { params: { path: { pageID: plans.id } } }));
    expect(versions.map((each) => each.number)).toEqual([3, 2, 1]);
  });

  test("discarding a draft leaves the page as published", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Discard");
    const plans = await createPage(api, space.homePageId, "Plans", doc("Kept words."));
    page.on("dialog", (dialog) => void dialog.accept());
    await page.goto(`/s/${space.key}/p/${plans.id}/plans/edit`);
    await typeIntoDraft(page, " Thrown away.");
    await page.locator('[data-action="discard-draft"]').click();
    await expect(page).toHaveURL(new RegExp(`/p/${plans.id}/plans$`));
    await expect(page.locator("[data-doc]")).toContainText("Kept words.");
    await expect(page.locator("[data-doc]")).not.toContainText("Thrown away.");
    expect(must(await api.GET("/pages/{pageID}/draft", { params: { path: { pageID: plans.id } } })).draft).toBeNull();
  });

  test("history compares two versions and restores an old one", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "History");
    const plans = await createPage(api, space.homePageId, "Plans", doc("The plan is one."));
    await publishVersion(api, plans.id, "The plan is two.", "Second go.");
    await publishVersion(api, plans.id, "The plan is three.", "Third go.");
    page.on("dialog", (dialog) => void dialog.accept());

    await page.goto(`/s/${space.key}/p/${plans.id}/plans/history`);
    await expect(versionRow(page, 3)).toContainText("Third go.");
    await versionRow(page, 1).getByRole("checkbox").check();
    await versionRow(page, 3).getByRole("checkbox").check();
    await page.locator('[data-action="compare-selected"]').click();
    await expect(page).toHaveURL(/\/history\?from=1&to=3$/);
    await expect(page.locator("[data-diff-view] del[data-diff]").filter({ hasText: "one" }).first()).toBeVisible();
    await expect(page.locator("[data-diff-view] ins[data-diff]").filter({ hasText: "three" }).first()).toBeVisible();

    await page.goto(`/s/${space.key}/p/${plans.id}/plans/history?version=1`);
    await expect(heading(page)).toHaveText("Plans, version 1");
    await expect(page.locator("[data-doc]")).toContainText("The plan is one.");
    await expect(page.locator("[contenteditable]")).toHaveCount(0);
    await page.locator('[data-action="restore-version"]').click();
    await expect(page.locator("[data-history-notice]")).toHaveText("Restored version 1 as version 4.");

    await page.goto(`/s/${space.key}/p/${plans.id}/plans/history`);
    await expect(versionRow(page, 4)).toContainText("Restored from version 1");
    await expect(versionRow(page, 4)).toContainText("Latest");
    await page.goto(`/s/${space.key}/p/${plans.id}/plans`);
    await expect(page.locator("[data-doc]")).toContainText("The plan is one.");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the editor, history and comparison pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      test.slow();
      const space = await freshSpace(api, testInfo, `Axe ${scheme}`);
      const plans = await createPage(api, space.homePageId, "Plans", doc("The plan is one.", "It stays."));
      await publishVersion(api, plans.id, "The plan is two.", "Second go.");
      const params = { path: { pageID: plans.id } };
      must(
        await api.PUT("/pages/{pageID}/draft", { params, body: { title: "Plans, drafted", body: doc("The plan is a draft.", "New line."), baseVersion: 2 } }),
      );
      await startInScheme(page, scheme);
      const base = `/s/${space.key}/p/${plans.id}/plans`;

      await page.goto(base);
      await expect(page.locator("[data-draft-note]")).toBeVisible();
      await expectAccessible(page);

      await page.goto(`${base}/edit`);
      await expect(page.locator("#page-body")).toContainText("The plan is a draft.");
      await page.locator('[data-action="publish"]').click();
      await expect(page.locator("[data-publish-dialog]")).toBeVisible();
      await expectAccessible(page);
      await page.keyboard.press("Escape");

      await page.goto(`${base}/history`);
      await expect(versionRow(page, 2)).toBeVisible();
      await expectAccessible(page);

      await page.goto(`${base}/history?version=1`);
      await expect(page.locator("[data-doc]")).toContainText("The plan is one.");
      await expectAccessible(page);

      await page.goto(`${base}/history?from=1&to=draft`);
      await expect(page.locator("[data-diff-view] ins[data-diff]").first()).toBeVisible();
      await expect(page.locator("[data-diff-view] del[data-diff]").first()).toBeVisible();
      await expectAccessible(page);
    });
  }
});
