import type { Locator, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { lapseVerification } from "../fixtures/db";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openPage, openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

const badge = (page: Page, state: "verified" | "expired") => page.locator(`main button[data-verification-badge="${state}"]`);
const list = (page: Page, name: string) => page.locator(`[data-home-list="${name}"]`);
const dialogOf = (page: Page, target: WikiPage) => page.getByRole("dialog", { name: `Owner and verification of ${target.title}` });
const bell = (page: Page) => page.locator('[data-action="notifications"]');
const panel = (page: Page) => page.locator("[data-notification-panel]");

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

// The compose worker looks for lapses every ten seconds; the fan-out and a
// replica that lags come on top.
const LAPSE_TOLD_MS = 60_000;

async function openPageShowing(page: Page, spaceKey: string, target: WikiPage, shown: (page: Page) => Locator) {
  await openShowing(page, `/s/${spaceKey}/p/${target.id}/page`, shown(page));
}

async function publishAs(api: StatorApi, target: WikiPage, text: string) {
  const { page } = must(await api.GET("/pages/{pageID}", { params: { path: { pageID: target.id } } }));
  must(
    await api.PUT("/pages/{pageID}/draft", {
      params: { path: { pageID: target.id } },
      body: { title: target.title, body: doc(text), baseVersion: page.version },
    }),
  );
  must(await api.POST("/pages/{pageID}/publish", { params: { path: { pageID: target.id } }, body: { notifyWatchers: false } }));
}

test.describe("owners and verified pages", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshPage(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, label));
    const target = await createPage(api, space.homePageId, uniqueName(testInfo, label), doc("Checked words."));
    return { space, target };
  }

  test("alice names bob the owner and verifies the page, which shows as verified on it, in search and at home, also after an edit", async ({
    page,
    api,
    apiAs,
    pageAs,
  }, testInfo) => {
    const { space, target } = await freshPage(api, testInfo, "Guide");
    const bobMe = must(await (await apiAs("bob")).GET("/auth/me")).user;

    await openPage(page, space.key, target);
    await page.locator('[data-action="page-menu"]').click();
    await page.getByRole("menuitem", { name: "Owner and verification" }).click();
    const dialog = dialogOf(page, target);
    await expect(dialog.getByText("Nobody owns this page yet.")).toBeVisible();
    await dialog.getByRole("combobox", { name: "Choose the owner" }).fill(bobMe.name.slice(0, 3));
    await dialog.getByRole("option", { name: new RegExp(bobMe.name) }).click();
    await expect(dialog.locator("[data-owner]")).toHaveText(bobMe.name);
    await dialog.getByRole("combobox", { name: "Valid for" }).selectOption("30");
    await dialog.getByRole("button", { name: "Verify", exact: true }).click();
    await expect(dialog.locator('[data-verification-state="verified"]')).toContainText("Verified by");
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(badge(page, "verified")).toHaveText("Verified");
    await expect(page.locator("[data-page-owner]")).toHaveText(`Owner: ${bobMe.name}`);
    expect(await scrollsSideways(page)).toBe(false);

    const bob = await pageAs("bob");
    const hit = bob.locator(`[data-search-hit="${target.title}"]`);
    await openShowing(bob, `/search?q=${encodeURIComponent(target.title)}`, hit.locator('[data-verification-badge="verified"]'));
    expect(await scrollsSideways(bob)).toBe(false);
    const update = list(bob, "updates").locator("li").filter({ hasText: target.title });
    await openShowing(bob, "/", update.locator('[data-verification-badge="verified"]'));

    await expect(async () => publishAs(api, target, "Changed after the check.")).toPass();
    await openPageShowing(page, space.key, target, (p) => p.getByText("Changed after the check."));
    await badge(page, "verified").click();
    await expect(dialogOf(page, target).locator("[data-changed-since]")).toHaveText("The check was of version 1; the page is at version 2 now.");
  });

  test("bob, who may only read the page, sees who owns it and who verified it and changes neither", async ({ api, apiAs, pageAs }, testInfo) => {
    const { space, target } = await freshPage(api, testInfo, "Read only");
    const alice = must(await api.GET("/auth/me")).user;
    must(
      await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: target.id } }, body: { view: [], edit: [{ type: "user", id: alice.id }] } }),
    );
    must(await api.PUT("/pages/{pageID}/owner", { params: { path: { pageID: target.id } }, body: { userId: alice.id } }));
    must(await api.PUT("/pages/{pageID}/verification", { params: { path: { pageID: target.id } }, body: { days: 90 } }));

    const bob = await pageAs("bob");
    await openPageShowing(bob, space.key, target, (p) => badge(p, "verified"));
    await expect(bob.locator("[data-page-owner]")).toHaveText(`Owner: ${alice.name}`);
    await bob.locator('[data-action="page-menu"]').click();
    await expect(bob.getByRole("menuitem", { name: "Owner and verification" })).toHaveCount(0);
    await bob.keyboard.press("Escape");
    await badge(bob, "verified").click();
    const dialog = dialogOf(bob, target);
    await expect(dialog.getByText("Only people who may edit this page can change its owner or verify it.")).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Verify again" })).toHaveCount(0);
    await expect(dialog.getByRole("combobox")).toHaveCount(0);

    const bobApi = await apiAs("bob");
    const refused = await bobApi.PUT("/pages/{pageID}/verification", { params: { path: { pageID: target.id } }, body: { days: 30 } });
    expect(refused.response.status).toBe(403);
    expect((refused.error as { error: { message: string } }).error.message).toMatch(/^You may read this page but not change it\./);
  });

  test("a verification that runs out shows as expired and reminds its owner", async ({ page, api, apiAs, pageAs }, testInfo) => {
    const { space, target } = await freshPage(api, testInfo, "Lapsing");
    const bobMe = must(await (await apiAs("bob")).GET("/auth/me")).user;
    must(await api.PUT("/pages/{pageID}/owner", { params: { path: { pageID: target.id } }, body: { userId: bobMe.id } }));
    must(await api.PUT("/pages/{pageID}/verification", { params: { path: { pageID: target.id } }, body: { days: 30 } }));
    await lapseVerification(target.id);

    await openPageShowing(page, space.key, target, (p) => badge(p, "expired"));
    await expect(badge(page, "expired")).toHaveText("Verification expired");

    const bob = await pageAs("bob");
    const told = panel(bob).locator('[data-notification="expired"]');
    await expect(async () => {
      await bob.goto("/");
      await bell(bob).click();
      await expect(told).toContainText(`The verification of ${target.title} has run out.`, ONE_LOOK);
    }).toPass({ timeout: LAPSE_TOLD_MS });
    await told.click();
    await expect(bob).toHaveURL(new RegExp(`/s/${space.key}/p/${target.id}/`));
    await expect(badge(bob, "expired")).toBeVisible();

    await openPage(page, space.key, target);
    await page.locator('[data-action="page-menu"]').click();
    await page.getByRole("menuitem", { name: "Owner and verification" }).click();
    await dialogOf(page, target).getByRole("button", { name: "Verify again" }).click();
    await expect(badge(page, "verified")).toBeVisible();
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the badge and the dialog are accessible and keyboard usable in ${scheme}`, async ({ page, api }, testInfo) => {
      const { space, target } = await freshPage(api, testInfo, `Axe ${scheme}`);
      const alice = must(await api.GET("/auth/me")).user;
      must(await api.PUT("/pages/{pageID}/owner", { params: { path: { pageID: target.id } }, body: { userId: alice.id } }));
      must(await api.PUT("/pages/{pageID}/verification", { params: { path: { pageID: target.id } }, body: { days: 30 } }));
      await startInScheme(page, scheme);

      await openPageShowing(page, space.key, target, (p) => badge(p, "verified"));
      await expectAccessible(page);
      await badge(page, "verified").focus();
      await page.keyboard.press("Enter");
      const dialog = dialogOf(page, target);
      await expect(dialog).toBeVisible();
      await expectAccessible(page);

      const term = dialog.getByRole("combobox", { name: "Valid for" });
      await term.focus();
      await term.selectOption("180");
      await page.keyboard.press("Tab");
      await expect(dialog.getByRole("button", { name: "Verify again" })).toBeFocused();
      // The dialog says verified before and after, and a verification landing
      // after the lapse below would undo it, so the answer is waited for.
      const reverified = page.waitForResponse((res) => res.url().endsWith(`/pages/${target.id}/verification`) && res.request().method() === "PUT");
      await page.keyboard.press("Enter");
      expect((await reverified).ok()).toBe(true);
      await expect(dialog.locator('[data-verification-state="verified"]')).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(dialog).toHaveCount(0);
      await expect(badge(page, "verified")).toBeFocused();

      await lapseVerification(target.id);
      await openPageShowing(page, space.key, target, (p) => badge(p, "expired"));
      await expectAccessible(page);
    });
  }
});
