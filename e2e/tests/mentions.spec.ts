import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

// Delivery goes through the outbox and the worker.
const DELIVERY_MS = 30_000;
const BOB = "Bob Builder";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const bell = (page: Page) => page.locator('[data-action="notifications"]');
const panel = (page: Page) => page.locator("[data-notification-panel]");
const mentionList = (page: Page) => page.getByRole("listbox", { name: "People to mention" });

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** Opens a page by id, waiting out a replica that has not seen it yet. */
async function openPage(page: Page, spaceKey: string, target: WikiPage) {
  await expect(async () => {
    await page.goto(`/s/${spaceKey}/p/${target.id}/page`);
    await expect(heading(page)).toHaveText(target.title, { timeout: 1_000 });
  }).toPass();
}

/** Opens the page's editor with the caret at the end of its words. */
async function editPage(page: Page) {
  await page.locator('[data-action="edit-page"]').click();
  await caretTo(page.locator("#page-body"), "end");
}

/** Types an at sign and a name, and waits for the person to be offered. */
async function offer(page: Page, query: string, name: string) {
  await page.keyboard.type(`@${query}`);
  const option = mentionList(page).getByRole("option", { name: new RegExp(name) });
  await expect(option).toBeVisible();
  return option;
}

async function publish(page: Page) {
  await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
  await page.locator('[data-action="publish"]').click();
  await page.locator("[data-publish-dialog]").locator('[data-action="confirm-publish"]').click();
  await page.waitForURL((url) => !url.pathname.endsWith("/edit"));
}

/** Waits for bob's notification about a page, reloading as a returning reader would, and opens the panel on it. */
async function toldAbout(page: Page, title: string) {
  const told = panel(page).locator('[data-notification="mentioned"]', { hasText: title });
  await expect(async () => {
    await page.reload();
    await bell(page).click();
    await expect(told).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: DELIVERY_MS });
  return told;
}

test.describe("mentions", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("alice mentions bob in a page, and bob is told and follows it there", async ({ page, api, pageAs }, testInfo) => {
    test.slow();
    const space = await freshSpace(api, testInfo, "Mention");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Plan"), doc("The plan."));

    await openPage(page, space.key, plan);
    await editPage(page);
    await page.keyboard.type(" Owner: ");
    const option = await offer(page, "bob", BOB);
    await expect(option).not.toHaveAttribute("data-cannot-view");
    await page.keyboard.press("Enter");
    await expect(mentionList(page)).toHaveCount(0);
    await expect(page.locator("#page-body [data-mention]")).toHaveText(`@${BOB}`);
    await publish(page);
    await expect(page.locator("main [data-mention]")).toHaveText(`@${BOB}`);

    const bob = await pageAs("bob");
    await openPage(bob, space.key, plan);
    const told = await toldAbout(bob, plan.title);
    await expect(told).toContainText(`mentioned you on ${plan.title}`);
    await expect(told).toContainText(`Owner: @${BOB}`);
    await told.click();
    await expect(bob).toHaveURL(new RegExp(`/s/${space.key}/p/${plan.id}/`));
    await expect(bob.locator("main [data-mention]")).toHaveText(`@${BOB}`);
  });

  test("alice mentions bob in a comment, and bob follows it to the thread", async ({ page, api, pageAs }, testInfo) => {
    test.slow();
    const space = await freshSpace(api, testInfo, "Comment");
    const notes = await createPage(api, space.homePageId, uniqueName(testInfo, "Notes"), doc("Notes."));

    await openPage(page, space.key, notes);
    await page.locator('[data-comments] [data-action="add-comment"]').click();
    await page.locator("#new-comment").click();
    await page.keyboard.type("Can you check this, ");
    await offer(page, "bo", BOB);
    await page.keyboard.press("Enter");
    await expect(page.locator("#new-comment [data-mention]")).toHaveText(`@${BOB}`);
    await page.keyboard.press("ControlOrMeta+Enter");
    const thread = page.locator("[data-comments] [data-thread]");
    await expect(thread).toHaveCount(1);
    await expect(thread.locator("[data-mention]")).toHaveText(`@${BOB}`);
    const threadId = await thread.getAttribute("data-thread");

    const bob = await pageAs("bob");
    await bob.goto("/spaces");
    const told = await toldAbout(bob, notes.title);
    await expect(told).toContainText(`mentioned you in a comment on ${notes.title}`);
    await told.click();
    await expect(bob).toHaveURL(new RegExp(`/s/${space.key}/p/${notes.id}/[^?]+\\?thread=${threadId}`));
    await expect(bob.locator(`[data-thread="${threadId}"]`)).toBeFocused();
  });

  test("the list marks somebody who cannot see the page, and they may still be named", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Private");
    const secret = await createPage(api, space.homePageId, uniqueName(testInfo, "Secret"), doc("Secret."));
    const me = must(await api.GET("/auth/me")).user.id;
    must(await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: secret.id } }, body: { view: [{ type: "user", id: me }], edit: [] } }));

    await openPage(page, space.key, secret);
    await editPage(page);
    await page.keyboard.type(" Ask ");
    const option = await offer(page, "bob", BOB);
    await expect(option).toHaveAttribute("data-cannot-view");
    await expect(option).toContainText("cannot see this page");
    await expect(mentionList(page).getByRole("option", { name: /Alice/ })).toHaveCount(0);
    await page.keyboard.press("Enter");
    await expect(page.locator("#page-body [data-mention]")).toHaveText(`@${BOB}`);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the mention list is accessible in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, `Axe ${scheme}`);
      const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Checked"), doc("Words."));
      const me = must(await api.GET("/auth/me")).user.id;
      must(await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: target.id } }, body: { view: [{ type: "user", id: me }], edit: [] } }));
      await startInScheme(page, scheme);
      await openPage(page, space.key, target);
      await editPage(page);
      await page.keyboard.type(" ");
      const option = await offer(page, "", BOB);
      await expect(option).toContainText("cannot see this page");
      await expect(page.locator("#page-body")).toHaveAttribute("aria-controls", (await mentionList(page).getAttribute("id"))!);
      await expectAccessible(page);
      await page.keyboard.press("ArrowDown");
      await page.keyboard.press("Escape");
      await expect(mentionList(page)).toHaveCount(0);
    });
  }
});
