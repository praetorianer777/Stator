import type { Locator, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { ageQuietly, lapseVerification } from "../fixtures/db";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const STALE_PATH = "/settings/stale";
// web/src/config.ts STALE_DEFAULT_DAYS and one of STALE_AGE_DAYS below it.
const DEFAULT_DAYS = 180;
const SHORTER_DAYS = 90;
const MAX_TABS = 60;

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const row = (page: Page, title: string) => page.locator(`[data-stale-row="${title}"]`);
const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });

/** Presses Tab until the control has focus, as a person working the page by keyboard would. */
async function tabTo(page: Page, control: Locator): Promise<void> {
  for (let i = 0; i < MAX_TABS; i++) {
    if (await control.evaluate((el) => el === document.activeElement)) return;
    await page.keyboard.press("Tab");
  }
  await expect(control).toBeFocused();
}

/** Opens the report on one space until it lists the page, which the replica may still be catching up on. */
async function openReport(page: Page, spaceKey: string, title: string): Promise<void> {
  await expect(async () => {
    await page.goto(`${STALE_PATH}?space=${spaceKey}`);
    await expect(row(page, title)).toBeVisible({ timeout: 1_000 });
  }).toPass();
}

test.describe("the stale content report", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  /** A space with a page untouched for a year, owned by bob and its verification run out, one untouched for a season, and a fresh one. */
  async function quietSpace(api: StatorApi, testInfo: TestInfo, bobId: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Quiet"));
    const guide = await createPage(api, space.homePageId, uniqueName(testInfo, "Old guide"), doc("Written long ago."));
    const notes = await createPage(api, space.homePageId, uniqueName(testInfo, "Season notes"), doc("Written a while ago."));
    const fresh = await createPage(api, space.homePageId, uniqueName(testInfo, "Fresh"), doc("Written today."));
    must(await api.PUT("/pages/{pageID}/owner", { params: { path: { pageID: guide.id } }, body: { userId: bobId } }));
    must(await api.PUT("/pages/{pageID}/verification", { params: { path: { pageID: guide.id } }, body: { days: 30 } }));
    await lapseVerification(guide.id);
    await ageQuietly(guide.id, 400);
    await ageQuietly(notes.id, 120);
    return { space, guide, notes, fresh };
  }

  test("an administrator opens it, narrows it to a space and a period, reads owner and verification, and reviews a page", async ({
    page,
    api,
    apiAs,
  }, testInfo) => {
    const bob = must(await (await apiAs("bob")).GET("/auth/me")).user;
    const { space, guide, notes, fresh } = await quietSpace(api, testInfo, bob.id);

    await expect(async () => {
      await page.goto("/");
      await page.locator('[data-action="account"]').click();
      await page.locator('[role="menu"] [data-action="stale-pages"]').click();
      await expect(page).toHaveURL(/\/settings\/stale$/);
      await page.locator("#stale-space").selectOption(space.key, { timeout: 1_000 });
      await expect(row(page, guide.title)).toBeVisible({ timeout: 1_000 });
    }).toPass();
    await expect(heading(page)).toHaveText("Stale pages");
    await expect(row(page, notes.title)).toHaveCount(0);
    await expect(row(page, fresh.title)).toHaveCount(0);
    await expect(page.locator("#stale-age")).toHaveValue(String(DEFAULT_DAYS));

    await page.locator("#stale-age").selectOption(String(SHORTER_DAYS));
    await expect(page.locator("[data-stale-row]")).toHaveCount(2);
    await expect(page.locator("[data-stale-row]").first()).toHaveAttribute("data-stale-row", guide.title);
    await expect(row(page, notes.title)).toBeVisible();
    await expect(row(page, fresh.title)).toHaveCount(0);

    const quiet = row(page, guide.title);
    await expect(quiet.locator("[data-stale-owner]")).toContainText(bob.name);
    await expect(quiet.locator('[data-stale-verification="expired"]')).toContainText("Expired");
    await expect(quiet.locator("[data-stale-viewed]")).toHaveText("Never");
    await expect(row(page, notes.title).locator("[data-stale-owner]")).toHaveText("Nobody");
    expect(await scrollsSideways(page)).toBe(false);

    await quiet.getByRole("button", { name: `Show only pages ${bob.name} owns` }).click();
    await expect(page.locator("#stale-owner")).toHaveValue(bob.id);
    await expect(page.locator("[data-stale-row]")).toHaveCount(1);

    await quiet.getByRole("link", { name: guide.title }).click();
    await expect(page).toHaveURL(new RegExp(`/s/${space.key}/p/${guide.id}/[^?]*\\?from=stale`));
    await expect(heading(page)).toHaveText(guide.title);
    await expect(page.getByText("Written long ago.")).toBeVisible();

    // The review was not a view: the page is still on the report.
    await openReport(page, space.key, guide.title);
    await expect(row(page, guide.title).locator("[data-stale-viewed]")).toHaveText("Never");

    await page.goto(`/s/${space.key}/settings`);
    await page.locator('[data-action="space-stale"]').click();
    await expect(page).toHaveURL(new RegExp(`/settings/stale\\?space=${space.key}$`));
    await expect(page.locator("#stale-space")).toHaveValue(space.key);
    await expect(row(page, guide.title)).toBeVisible();
  });

  test("a member who administers no space is refused, and is not offered it", async ({ api, apiAs, pageAs }, testInfo) => {
    const bobApi = await apiAs("bob");
    const bob = must(await bobApi.GET("/auth/me")).user;
    const { space } = await quietSpace(api, testInfo, bob.id);

    const refused = await bobApi.GET("/stale-pages", { params: { query: { space: space.key } } });
    expect(refused.response.status).toBe(403);

    const member = await pageAs("bob");
    await member.goto(STALE_PATH);
    await expect(member.locator("[data-stale-refused]")).toContainText("Only administrators of a space, or of the organization, read which pages went stale.");
    await expect(member.locator("[data-stale-table]")).toHaveCount(0);
    await member.locator('[data-action="account"]').click();
    await expect(member.locator('[role="menu"]')).toBeVisible();
    await expect(member.locator('[role="menu"] [data-action="stale-pages"]')).toHaveCount(0);
    expect(await scrollsSideways(member)).toBe(false);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the report is accessible and works from the keyboard in ${scheme}`, async ({ page, api, apiAs }, testInfo) => {
      const bob = must(await (await apiAs("bob")).GET("/auth/me")).user;
      const { space, guide, notes } = await quietSpace(api, testInfo, bob.id);
      await startInScheme(page, scheme);

      await openReport(page, space.key, guide.title);
      await expectAccessible(page);

      const age = page.locator("#stale-age");
      await tabTo(page, age);
      await age.selectOption(String(SHORTER_DAYS));
      await expect(row(page, notes.title)).toBeVisible();
      await expectAccessible(page);

      const link = row(page, guide.title).getByRole("link", { name: guide.title });
      await tabTo(page, link);
      await page.keyboard.press("Enter");
      await expect(heading(page)).toHaveText(guide.title);
    });
  }
});
