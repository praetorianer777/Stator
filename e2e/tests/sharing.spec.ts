import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { withDatabase } from "../fixtures/db";
import { mailsTo, mailText } from "../fixtures/mail";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openPage } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const BOB = "Bob Builder";
const BOB_EMAIL = "bob@stator.test";
// Delivery goes through the outbox and the worker, and mail through Mailpit.
const DELIVERY_MS = 30_000;
// How long a notice that should not come is given to come anyway.
const QUIET_MS = 3_000;

const shareButton = (page: Page) => page.locator('[data-action="share-page"]');
const dialog = (page: Page) => page.locator("[data-share-dialog]");
const picker = (page: Page) => dialog(page).getByRole("combobox", { name: "Send to" });
const bell = (page: Page) => page.locator('[data-action="notifications"]');
const badge = (page: Page) => page.locator("[data-unread-badge]");
const panel = (page: Page) => page.locator("[data-notification-panel]");

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** Types into the picker until the option shows, and picks it. */
async function pick(page: Page, typed: string, name: string) {
  await picker(page).fill(typed);
  await dialog(page)
    .getByRole("option", { name: new RegExp(`^${name}`) })
    .click();
}

test.describe("sharing a page", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("alice shares a page with bob and a group of his with a note, and bob is told in the app and by mail", async ({
    page,
    api,
    apiAs,
    pageAs,
    freshOrg,
  }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Share");
    const runbook = await createPage(api, space.homePageId, uniqueName(testInfo, "Runbook"), doc("Rollback steps."));
    const bobApi = await apiAs("bob");
    const bobId = must(await bobApi.GET("/auth/me")).user.id;
    const groupName = uniqueName(testInfo, "Readers");
    await withDatabase(async (db) => {
      const { rows } = await db.query<{ id: string }>("INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id", [freshOrg.id, groupName]);
      await db.query("INSERT INTO group_member (org_id, group_id, user_id) VALUES ($1, $2, $3)", [freshOrg.id, rows[0]!.id, bobId]);
    });

    await openPage(page, space.key, runbook);
    await shareButton(page).click();
    await expect(dialog(page).getByRole("heading", { name: `Share ${runbook.title}` })).toBeVisible();
    await expect(dialog(page).locator("[data-viewers-everyone]")).toHaveText("Everyone in the organization can view this page.");
    await pick(page, "bob", BOB);
    await expect(async () => {
      await picker(page).fill(groupName.slice(0, -3));
      await expect(dialog(page).getByRole("option", { name: new RegExp(`^${groupName}`) })).toBeVisible(ONE_LOOK);
    }).toPass();
    await dialog(page)
      .getByRole("option", { name: new RegExp(`^${groupName}`) })
      .click();
    await expect(dialog(page).locator("[data-share-recipient]")).toHaveCount(2);
    await expect(dialog(page).locator("[data-share-closed-note]")).toHaveCount(0);
    await dialog(page).getByRole("textbox", { name: "Note (optional)" }).fill("Read the rollback part before Friday.");
    await dialog(page).locator('[data-action="send-share"]').click();
    await expect(dialog(page).locator("[data-share-sent]")).toHaveText("Shared with 1 person.");
    await dialog(page).locator('[data-action="close-share"]').click();
    await expect(dialog(page)).toHaveCount(0);
    await expect(shareButton(page)).toBeFocused();

    const bob = await pageAs("bob");
    await openPage(bob, space.key, runbook);
    await expect(async () => {
      await bob.reload();
      await expect(badge(bob)).toHaveText("1", ONE_LOOK);
    }).toPass({ timeout: DELIVERY_MS });
    await bell(bob).click();
    const told = panel(bob).locator('[data-notification="shared"]');
    await expect(told).toContainText(`shared ${runbook.title} with you`);
    await expect(told).toContainText("Read the rollback part before Friday.");
    await told.click();
    await expect(bob).toHaveURL(new RegExp(`/s/${space.key}/p/${runbook.id}/`));

    await expect.poll(async () => (await mailsTo(BOB_EMAIL, runbook.title)).length, { timeout: DELIVERY_MS }).toBe(1);
    const [mail] = await mailsTo(BOB_EMAIL, runbook.title);
    expect(mail!.Subject).toContain(`shared "${runbook.title}" with you`);
    const text = await mailText(mail!.ID);
    expect(text).toContain("Read the rollback part before Friday.");
    expect(text).toContain(`/s/${space.key}/p/${runbook.id}`);
    expect((await bobApi.POST("/notifications/read", { body: { all: true } })).response.status).toBe(204);
  });

  test("a page closed to bob is not shared with him, and he hears nothing", async ({ page, api, apiAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Closed");
    const secret = await createPage(api, space.homePageId, uniqueName(testInfo, "Secret"), doc("Hidden."));
    const alice = must(await api.GET("/auth/me")).user;
    must(
      await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: secret.id } }, body: { view: [{ type: "user", id: alice.id }], edit: [] } }),
    );

    await openPage(page, space.key, secret);
    await shareButton(page).click();
    await expect(dialog(page).locator("[data-viewers-total]")).toHaveText("This page is restricted. 1 person can view it:");
    await picker(page).fill("bob");
    const option = dialog(page).getByRole("option", { name: new RegExp(`^${BOB}`) });
    await expect(option).toHaveAttribute("data-option-closed", "");
    await expect(option).toContainText("Cannot view this page");
    await option.click();
    await expect(dialog(page).locator(`[data-share-recipient="${BOB}"]`)).toHaveAttribute("data-closed", "");
    await expect(dialog(page).locator("[data-share-closed-note]")).toContainText("Sharing does not give access.");
    await dialog(page).locator('[data-action="send-share"]').click();
    await expect(dialog(page).locator("[data-share-error]")).toHaveText(/^Nothing was shared, because the page is closed to somebody you picked\./);
    await expect(dialog(page).locator("[data-share-sent]")).toHaveCount(0);

    const bobApi = await apiAs("bob");
    await page.waitForTimeout(QUIET_MS);
    const told = must(await bobApi.GET("/notifications", { params: { query: { limit: 100 } } })).notifications;
    expect(told.filter((n) => n.page.id === secret.id)).toEqual([]);
    expect(await mailsTo(BOB_EMAIL, secret.title)).toEqual([]);
  });

  test("the dialog works from the keyboard alone", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Keys");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Plan"), doc("Words."));
    await openPage(page, space.key, plan);
    await shareButton(page).focus();
    await page.keyboard.press("Enter");
    await expect(dialog(page)).toBeVisible();
    await picker(page).focus();
    await page.keyboard.type("bob");
    await expect(dialog(page).getByRole("option", { name: new RegExp(`^${BOB}`) })).toBeVisible();
    await page.keyboard.press("Enter");
    await expect(dialog(page).locator(`[data-share-recipient="${BOB}"]`)).toBeVisible();
    await expect(dialog(page)).toBeVisible();
    await dialog(page).getByRole("textbox", { name: "Note (optional)" }).focus();
    await page.keyboard.type("Have a look.");
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toHaveCount(0);
    await expect(shareButton(page)).toBeFocused();
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the share dialog is accessible in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, `Axe ${scheme}`);
      const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Checked"), doc("Words."));
      const alice = must(await api.GET("/auth/me")).user;
      must(
        await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: plan.id } }, body: { view: [{ type: "user", id: alice.id }], edit: [] } }),
      );
      await startInScheme(page, scheme);
      await openPage(page, space.key, plan);
      await shareButton(page).click();
      // Checked with the list open on bob, who may not view the page: the org
      // holds nobody else to offer once he is picked.
      await picker(page).fill("bob");
      const option = dialog(page).getByRole("option", { name: new RegExp(`^${BOB}`) });
      await expect(option).toContainText("Cannot view this page");
      await expectAccessible(page);
      await option.click();
      await expect(dialog(page).locator("[data-share-closed-note]")).toBeVisible();
      await expectAccessible(page);
    });
  }

  test("the dialog fits a narrow screen", { tag: "@mobile" }, async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Narrow");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Plan"), doc("Words."));
    await openPage(page, space.key, plan);
    await expect(shareButton(page)).toBeVisible();
    expect(await scrollsSideways(page)).toBe(false);
    await shareButton(page).click();
    await pick(page, "bob", BOB);
    await expect(dialog(page).locator('[data-action="send-share"]')).toBeInViewport();
    expect(await scrollsSideways(page)).toBe(false);
  });
});
