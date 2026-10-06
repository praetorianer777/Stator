import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { mailsTo, mailText } from "../fixtures/mail";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openPage } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const BOB_EMAIL = "bob@stator.test";
// Delivery goes through the outbox and the worker, and mail through Mailpit.
const DELIVERY_MS = 30_000;
// How long a mail that should not come is given to come anyway.
const QUIET_MS = 3_000;

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const watchButton = (page: Page) => page.locator('[data-action="watch-menu"]');
const bell = (page: Page) => page.locator('[data-action="notifications"]');
const badge = (page: Page) => page.locator("[data-unread-badge]");
const panel = (page: Page) => page.locator("[data-notification-panel]");

const doc = (text: string) => ({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** Publishes a new version through the API, as the editor does, telling the watchers. */
async function publishWithNotice(api: StatorApi, id: string, text: string): Promise<void> {
  const params = { path: { pageID: id } };
  const current = must(await api.GET("/pages/{pageID}", { params })).page;
  must(await api.PUT("/pages/{pageID}/draft", { params, body: { title: current.title, body: doc(text), baseVersion: current.version } }));
  must(await api.POST("/pages/{pageID}/publish", { params, body: { notifyWatchers: true } }));
}

/** Waits until the badge counts as many unread as expected, reloading as a returning reader would. */
async function expectBadge(page: Page, count: number) {
  await expect(async () => {
    await page.reload();
    await expect(badge(page)).toHaveText(String(count), ONE_LOOK);
  }).toPass({ timeout: DELIVERY_MS });
}

test.describe("watching and notifications", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("bob watches, alice publishes with a notice, and bob is told in the app and by mail", async ({ page, api, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Notify");
    const runbook = await createPage(api, space.homePageId, uniqueName(testInfo, "Runbook"), doc("First words."));

    const bob = await pageAs("bob");
    await openPage(bob, space.key, runbook);
    await expect(watchButton(bob)).toHaveText("Watch");
    await watchButton(bob).click();
    await bob.getByRole("menuitem", { name: "Watch this page", exact: true }).click();
    await expect(watchButton(bob)).toHaveText("Watching");

    await openPage(page, space.key, runbook);
    await page.locator('[data-action="edit-page"]').click();
    await caretTo(page.locator("#page-body"), "end");
    await page.keyboard.type(" Rollback steps added.");
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await page.locator('[data-action="publish"]').click();
    const dialog = page.locator("[data-publish-dialog]");
    await expect(dialog.getByRole("checkbox", { name: "Notify the people watching this page" })).toBeChecked();
    await dialog.locator('[data-action="confirm-publish"]').click();
    await page.waitForURL((url) => !url.pathname.endsWith("/edit"));

    await expectBadge(bob, 1);
    await expect(bell(bob)).toHaveAccessibleName("Notifications, 1 unread");
    await bell(bob).click();
    const told = panel(bob).locator('[data-notification="published"]');
    await expect(told).toContainText(`published version 2 of ${runbook.title}`);
    await expect(told).toHaveAttribute("data-unread", "true");
    await told.click();
    await expect(bob).toHaveURL(new RegExp(`/s/${space.key}/p/${runbook.id}/`));
    await expect(panel(bob)).toHaveCount(0);
    await expect(badge(bob)).toHaveCount(0);
    await bell(bob).click();
    await expect(panel(bob).locator('[data-notification="published"]')).not.toHaveAttribute("data-unread", "true");

    await expect.poll(async () => (await mailsTo(BOB_EMAIL, runbook.title)).length, { timeout: DELIVERY_MS }).toBe(1);
    const [mail] = await mailsTo(BOB_EMAIL, runbook.title);
    expect(mail!.Subject).toContain(`published a new version of "${runbook.title}"`);
    expect(await mailText(mail!.ID)).toContain(`/s/${space.key}/p/${runbook.id}`);
  });

  test("a kind switched off by email is still shown in the app, and mailed to nobody", async ({ api, apiAs, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Quiet");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Plan"), doc("First words."));
    const bobApi = await apiAs("bob");
    const saved = must(await bobApi.GET("/notification-preferences")).preferences;
    try {
      const bob = await pageAs("bob");
      await bob.goto("/settings/notifications");
      const email = bob.getByRole("checkbox", { name: "By email: A page you watch is published with a notice" });
      await expect(email).toBeChecked();
      await email.uncheck();
      await bob.getByRole("button", { name: "Save" }).click();
      await expect(bob.locator("[data-preferences-saved]")).toHaveText("Your notification settings are saved.");

      await expect.poll(async () => (await bobApi.PUT("/pages/{pageID}/watch", { params: { path: { pageID: plan.id } }, body: {} })).response.status).toBe(200);
      await publishWithNotice(api, plan.id, "Second words.");
      await openPage(bob, space.key, plan);
      await expectBadge(bob, 1);
      await bob.waitForTimeout(QUIET_MS);
      expect(await mailsTo(BOB_EMAIL, plan.title)).toEqual([]);
    } finally {
      must(await bobApi.PUT("/notification-preferences", { body: saved }));
      expect((await bobApi.POST("/notifications/read", { body: { all: true } })).response.status).toBe(204);
    }
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the watch menu, the notification panel and the settings are accessible in ${scheme}`, async ({ api, pageAs }, testInfo) => {
      const space = await freshSpace(api, testInfo, `Axe ${scheme}`);
      const target = await createPage(api, space.homePageId, uniqueName(testInfo, "Checked"), doc("Words."));
      const bob = await pageAs("bob");
      await startInScheme(bob, scheme);
      await openPage(bob, space.key, target);
      await watchButton(bob).click();
      await expect(bob.getByRole("menu")).toBeVisible();
      await expectAccessible(bob);
      await bob.keyboard.press("Escape");
      await expect(watchButton(bob)).toBeFocused();

      await bell(bob).focus();
      await bob.keyboard.press("Enter");
      await expect(panel(bob)).toBeVisible();
      await expectAccessible(bob);
      await bob.keyboard.press("Escape");
      await expect(bell(bob)).toBeFocused();

      await bob.goto("/settings/notifications");
      await expect(bob.getByRole("button", { name: "Save" })).toBeVisible();
      await expectAccessible(bob);
      await bob.goto("/settings/watching");
      await expect(heading(bob)).toHaveText("Watching");
      await expectAccessible(bob);
    });
  }
});
