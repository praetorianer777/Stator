import type { Locator, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { throwawayPerson } from "../fixtures/db";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const BOB = "Bob Builder";
const MAX_TABS = 40;

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const viewsButton = (page: Page) => page.locator("[data-page-views]");
const dialog = (page: Page) => page.locator("[data-page-views-dialog]");
const reader = (page: Page, name: string) => dialog(page).locator(`[data-reader="${name}"]`);

/** Opens a page by id, waiting out a replica that has not seen it yet. */
async function openPage(page: Page, spaceKey: string, target: WikiPage) {
  await expect(async () => {
    await page.goto(`/s/${spaceKey}/p/${target.id}/page`);
    await expect(heading(page)).toHaveText(target.title, { timeout: 1_000 });
  }).toPass();
}

/** Opens the page until its line under the title counts the views wanted, then opens them. */
async function openViews(page: Page, spaceKey: string, target: WikiPage, views: number) {
  await expect(async () => {
    await openPage(page, spaceKey, target);
    await expect(viewsButton(page)).toHaveAttribute("data-page-views", String(views), { timeout: 2_000 });
  }).toPass();
  await viewsButton(page).click();
  await expect(dialog(page).getByRole("heading", { name: "Page views" })).toBeVisible();
}

/** Presses Tab until the control has focus, as a person working the page by keyboard would. */
async function tabTo(page: Page, control: Locator): Promise<void> {
  for (let i = 0; i < MAX_TABS; i++) {
    if (await control.evaluate((el) => el === document.activeElement)) return;
    await page.keyboard.press("Tab");
  }
  await expect(control).toBeFocused();
}

test.describe("page views", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  /** A space everybody reads and only alice edits, with one page in it. */
  async function readOnlySpace(api: StatorApi, testInfo: TestInfo) {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "Read"));
    must(
      await api.PUT("/spaces/{spaceKey}/permissions", {
        params: { path: { spaceKey: key } },
        body: { grants: [{ subject: { type: "everyone" }, permissions: ["view"] }] },
      }),
    );
    const guide = await createPage(api, space.homePageId, uniqueName(testInfo, "Guide"), doc("How we work."));
    return { space, guide };
  }

  test("bob reads a page and sees how often it was read; alice, who edits it, also sees that bob read it", async ({ page, api, pageAs }, testInfo) => {
    const { space, guide } = await readOnlySpace(api, testInfo);
    const alice = must(await api.GET("/auth/me")).user;

    const bob = await pageAs("bob");
    await openViews(bob, space.key, guide, 1);
    await expect(dialog(bob).locator("[data-views-in-all]")).toContainText("1 view");
    await expect(dialog(bob).locator("[data-views-in-all]")).toContainText("1 person");
    await expect(dialog(bob).locator("[data-views-lately]")).toContainText("Last 30 days");
    await expect(dialog(bob).locator("[data-readers-editors-only]")).toHaveText("Only people who may edit this page see who read it.");
    await expect(dialog(bob).locator("[data-page-readers]")).toHaveCount(0);
    expect(await scrollsSideways(bob)).toBe(false);

    // Reading it again the same day is not another view.
    await openViews(bob, space.key, guide, 1);

    await openViews(page, space.key, guide, 2);
    await expect(dialog(page).locator("[data-views-in-all]")).toContainText("2 people");
    const readers = dialog(page).locator("[data-page-readers]");
    await expect(readers.getByRole("heading", { name: "Who read it" })).toBeVisible();
    await expect(readers.locator("[data-reader]")).toHaveCount(2);
    await expect(readers.locator("[data-reader]").first()).toHaveAttribute("data-reader", alice.name);
    await expect(reader(page, BOB)).toContainText("on 1 day");
    expect(await scrollsSideways(page)).toBe(false);
  });

  // Whether a name is shown belongs to the person in every organization, so
  // somebody of the spec's own hides theirs: bob hiding his would hide him
  // from every other spec running at the same time.
  test("a reader hides their name in their profile, and alice sees them counted but not named", async ({ page, api, browser, freshOrg }, testInfo) => {
    const { space, guide } = await readOnlySpace(api, testInfo);
    const person = await throwawayPerson(testInfo, "reader", freshOrg);
    try {
      const context = await browser.newContext();
      await context.addCookies([{ ...person.session, url: WEB_URL, httpOnly: true, sameSite: "Lax" }]);
      const them = await context.newPage();
      await openViews(them, space.key, guide, 1);

      await them.goto("/settings/profile");
      const toggle = them.getByRole("switch", { name: "Show my name to the editors of pages I read" });
      await expect(toggle).toHaveAttribute("aria-checked", "true");
      await toggle.click();
      await expect(toggle).toHaveAttribute("aria-checked", "false");
      await expect(them.locator("[data-privacy-saved]")).toHaveText("Your choice is saved.");
      await expect(them.getByText("You are still counted when you read a page, only without your name.")).toBeVisible();
      expect(await scrollsSideways(them)).toBe(false);
      await context.close();

      await expect(async () => {
        await openViews(page, space.key, guide, 2);
        await expect(dialog(page).locator("[data-readers-unnamed]")).toHaveText("1 more person chose not to be named.", { timeout: 1_000 });
      }).toPass();
      await expect(reader(page, person.name)).toHaveCount(0);
      await expect(dialog(page).locator("[data-reader]")).toHaveCount(1);
      await expect(dialog(page).locator("[data-views-in-all]")).toContainText("2 views");
    } finally {
      await person.remove();
    }
  });

  test("a page bob may not view tells him nothing of its views", async ({ page, api, apiAs, pageAs }, testInfo) => {
    const { space, guide } = await readOnlySpace(api, testInfo);
    const alice = must(await api.GET("/auth/me")).user;
    must(
      await api.PUT("/pages/{pageID}/restrictions", {
        params: { path: { pageID: guide.id } },
        body: { view: [{ type: "user", id: alice.id }], edit: [] },
      }),
    );
    await openViews(page, space.key, guide, 1);

    const bobApi = await apiAs("bob");
    await expect(async () => {
      expect((await bobApi.GET("/pages/{pageID}/views", { params: { path: { pageID: guide.id } } })).response.status).toBe(404);
    }).toPass();
    expect((await bobApi.GET("/pages/{pageID}/readers", { params: { path: { pageID: guide.id } } })).response.status).toBe(404);
    const bob = await pageAs("bob");
    // His browser's reads are not held to Alice's restriction, so a lagging replica may still show the page.
    await expect(async () => {
      await bob.goto(`/s/${space.key}/p/${guide.id}/page`);
      await expect(bob.locator("main")).not.toContainText(guide.title, { timeout: 2_000 });
    }).toPass();
    await expect(viewsButton(bob)).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the views open from the keyboard and are accessible in ${scheme}`, async ({ page, api }, testInfo) => {
      const { space, guide } = await readOnlySpace(api, testInfo);
      await startInScheme(page, scheme);
      await expect(async () => {
        await openPage(page, space.key, guide);
        await expect(viewsButton(page)).toHaveAttribute("data-page-views", "1", { timeout: 2_000 });
      }).toPass();
      await expectAccessible(page);

      await tabTo(page, viewsButton(page));
      await page.keyboard.press("Enter");
      await expect(dialog(page)).toBeVisible();
      await expect(dialog(page).locator("[data-reader]")).toHaveCount(1);
      await expectAccessible(page);
      await page.keyboard.press("Escape");
      await expect(dialog(page)).toHaveCount(0);
      await expect(viewsButton(page)).toBeFocused();
    });
  }
});
