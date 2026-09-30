import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

// Delivery goes through the outbox and the worker.
const DELIVERY_MS = 30_000;

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const comments = (page: Page) => page.locator("[data-comments]");
const threads = (page: Page) => comments(page).locator("[data-thread]");
const bell = (page: Page) => page.locator('[data-action="notifications"]');
const badge = (page: Page) => page.locator("[data-unread-badge]");
const panel = (page: Page) => page.locator("[data-notification-panel]");

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** Opens a page by id, waiting out a replica that has not seen it yet. */
async function openPage(page: Page, spaceKey: string, target: WikiPage) {
  await expect(async () => {
    await page.goto(`/s/${spaceKey}/p/${target.id}/page`);
    await expect(heading(page)).toHaveText(target.title, { timeout: 1_000 });
  }).toPass();
}

/** Writes in the comment editor that opened last and posts it. */
async function post(page: Page, editor: string, text: string) {
  const box = page.locator(`#${editor}`);
  await expect(box).toBeVisible();
  await box.click();
  await page.keyboard.type(text);
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(box).toHaveCount(0);
}

test.describe("comments below a page", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("alice comments, bob replies, and alice follows her notification to the thread", async ({ page, api, pageAs }, testInfo) => {
    test.slow();
    const space = await freshSpace(api, testInfo, "Talk");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Plan"), doc("The plan."));

    await openPage(page, space.key, plan);
    await comments(page).locator('[data-action="add-comment"]').click();
    await post(page, "new-comment", "Should we ship on Friday?");
    await expect(threads(page)).toHaveCount(1);
    await expect(threads(page).first()).toContainText("Should we ship on Friday?");
    await expect(page.locator("[data-comment-count]")).toHaveText("1 comment");

    const bob = await pageAs("bob");
    await expect(async () => {
      await openPage(bob, space.key, plan);
      await expect(threads(bob)).toHaveCount(1, { timeout: 1_000 });
    }).toPass();
    const thread = threads(bob).first();
    const threadId = await thread.getAttribute("data-thread");
    await thread.locator('[data-action="reply"]').click();
    await post(bob, `reply-${threadId}`, "Monday is safer.");
    await expect(thread.locator("[data-comment]")).toHaveCount(2);
    await expect(thread.locator("[data-comment]").last()).toContainText("Bob Builder");

    await page.goto("/spaces");
    const told = panel(page).locator('[data-notification="replied"]', { hasText: plan.title });
    await expect(async () => {
      await page.reload();
      await expect(badge(page)).toBeVisible({ timeout: 2_000 });
      await bell(page).click();
      await expect(told).toBeVisible({ timeout: 2_000 });
    }).toPass({ timeout: DELIVERY_MS });
    await expect(told).toContainText(`replied in a thread on ${plan.title}`);
    await expect(told).toContainText("Monday is safer.");
    await told.click();
    await expect(page).toHaveURL(new RegExp(`/s/${space.key}/p/${plan.id}/[^?]+\\?thread=${threadId}`));
    const target = comments(page).locator(`[data-thread="${threadId}"]`);
    await expect(target).toBeFocused();
    await expect(target).toBeInViewport();
    await expect(target).toContainText("Monday is safer.");
  });

  test("edit, delete and the placeholder a deleted comment leaves", async ({ page, api, apiAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Edit");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Notes"), doc("Notes."));
    const started = must(await api.POST("/pages/{pageID}/comments", { params: { path: { pageID: plan.id } }, body: { body: doc("First thought.") } })).thread;
    const bobApi = await apiAs("bob");
    await expect
      .poll(
        async () =>
          (await bobApi.POST("/comments/{commentID}/replies", { params: { path: { commentID: started.id } }, body: { body: doc("Bob agrees.") } })).response
            .status,
      )
      .toBe(201);

    await openPage(page, space.key, plan);
    const first = comments(page).locator(`[data-comment="${started.id}"]`);
    await expect(first).toContainText("First thought.");
    await first.locator('[data-action="edit-comment"]').click();
    const editor = page.locator(`#edit-${started.id}`);
    await editor.click();
    await page.keyboard.press("ControlOrMeta+End");
    await page.keyboard.type(" Revised.");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(first).toContainText("First thought. Revised.");
    await expect(first.locator("[data-comment-edited]")).toBeVisible();
    await expect(first.locator('[data-action="edit-comment"]')).toBeFocused();

    page.once("dialog", (dialog) => void dialog.accept());
    await first.locator('[data-action="delete-comment"]').click();
    await expect(first.locator("[data-comment-placeholder]")).toHaveText("This comment was deleted.");
    await expect(first).toContainText("Alice");
    await expect(comments(page).locator("[data-thread]")).toHaveCount(1);
    await expect(comments(page)).toContainText("Bob agrees.");
    await expect(page.locator("[data-comment-count]")).toHaveText("1 comment");

    const reply = comments(page).locator("[data-comment]").last();
    page.once("dialog", (dialog) => void dialog.accept());
    await reply.locator('[data-action="delete-comment"]').click();
    await expect(comments(page).locator("[data-thread]")).toHaveCount(0);
    await expect(comments(page).locator("[data-comments-empty]")).toBeVisible();
    await expect(page.locator("[data-comment-count]")).toHaveCount(0);
  });

  test("a member without comment rights reads the discussion and cannot add to it", async ({ api, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Read");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Read only"), doc("Read me."));
    must(await api.POST("/pages/{pageID}/comments", { params: { path: { pageID: plan.id } }, body: { body: doc("Alice was here.") } }));
    must(
      await api.PUT("/spaces/{spaceKey}/permissions", {
        params: { path: { spaceKey: space.key } },
        body: { grants: [{ subject: { type: "everyone" }, permissions: ["view"] }] },
      }),
    );

    const bob = await pageAs("bob");
    await expect(async () => {
      await openPage(bob, space.key, plan);
      await expect(comments(bob).locator("[data-comments-read-only]").first()).toBeVisible({ timeout: 1_000 });
    }).toPass();
    await expect(comments(bob)).toContainText("Alice was here.");
    await expect(comments(bob).locator('[data-action="add-comment"]')).toHaveCount(0);
    await expect(comments(bob).locator('[data-action="reply"]')).toHaveCount(0);
    await expect(comments(bob).locator('[data-action="delete-comment"]')).toHaveCount(0);
  });

  test("only an administrator of the space is offered Delete on somebody else's comment", async ({ api, apiAs, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Moderate");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Moderated"), doc("Words."));
    const started = must(await api.POST("/pages/{pageID}/comments", { params: { path: { pageID: plan.id } }, body: { body: doc("Alice's thought.") } })).thread;
    const bobId = String((await (await apiAs("bob")).GET("/auth/me")).data?.user.id);
    const everyoneMay = {
      subject: { type: "everyone" as const },
      permissions: ["view" as const, "addPages" as const, "addComments" as const, "delete" as const],
    };

    const bob = await pageAs("bob");
    const alices = comments(bob).locator(`[data-comment="${started.id}"]`);
    await expect(async () => {
      await openPage(bob, space.key, plan);
      await expect(alices).toContainText("Alice's thought.", { timeout: 1_000 });
    }).toPass();
    await expect(comments(bob).locator('[data-action="reply"]')).toBeVisible();
    await expect(alices.locator('[data-action="delete-comment"]')).toHaveCount(0);

    must(
      await api.PUT("/spaces/{spaceKey}/permissions", {
        params: { path: { spaceKey: space.key } },
        body: { grants: [everyoneMay, { subject: { type: "user", id: bobId }, permissions: ["administer"] }] },
      }),
    );
    await expect(async () => {
      await bob.reload();
      await expect(alices.locator('[data-action="delete-comment"]')).toBeVisible({ timeout: 1_000 });
    }).toPass();
    await expectAccessible(bob);
    bob.once("dialog", (dialog) => void dialog.accept());
    await alices.locator('[data-action="delete-comment"]').click();
    await expect(alices).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the comments are accessible and keyboard usable in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, `Axe ${scheme}`);
      const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Checked"), doc("Words."));
      const started = must(await api.POST("/pages/{pageID}/comments", { params: { path: { pageID: plan.id } }, body: { body: doc("A thought.") } })).thread;
      must(await api.POST("/comments/{commentID}/replies", { params: { path: { commentID: started.id } }, body: { body: doc("A reply.") } }));
      await startInScheme(page, scheme);
      await expect(async () => {
        await openPage(page, space.key, plan);
        await expect(threads(page).locator("[data-comment]")).toHaveCount(2, { timeout: 1_000 });
      }).toPass();
      await expectAccessible(page);

      const reply = threads(page).first().locator('[data-action="reply"]');
      await reply.focus();
      await page.keyboard.press("Enter");
      const box = page.locator(`#reply-${started.id}`);
      await expect(box).toBeFocused();
      await expectAccessible(page);
      await page.keyboard.press("Escape");
      await expect(box).toHaveCount(0);
      await expect(reply).toBeFocused();

      const add = comments(page).locator('[data-action="add-comment"]');
      await add.focus();
      await page.keyboard.press("Enter");
      await expect(page.locator("#new-comment")).toBeFocused();
      await page.keyboard.type("Typed without a mouse.");
      await page.keyboard.press("ControlOrMeta+Enter");
      await expect(threads(page)).toHaveCount(2);
      await expect(threads(page).last()).toBeFocused();
    });
  }
});
