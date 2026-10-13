import type { Locator, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { ONE_LOOK, openUntil, openWithout } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

const MAX_TABS = 40;

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });
const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const viewsButton = (page: Page) => page.locator("[data-page-views]");
const dialog = (page: Page) => page.locator("[data-page-views-dialog]");
const reader = (page: Page, name: string) => dialog(page).locator(`[data-reader="${name}"]`);

const pathOf = (spaceKey: string, target: WikiPage) => `/s/${spaceKey}/p/${target.id}/page`;

/** Opens the page until its line under the title counts the views wanted. */
async function openCounted(page: Page, spaceKey: string, target: WikiPage, views: number, then?: () => Promise<unknown>) {
  await openUntil(page, pathOf(spaceKey, target), async () => {
    await expect(heading(page)).toHaveText(target.title, ONE_LOOK);
    await expect(viewsButton(page)).toHaveAttribute("data-page-views", String(views), ONE_LOOK);
    await then?.();
  });
}

/** Opens the page until it counts the views wanted, then opens them, until `inDialog` holds. */
async function openViews(page: Page, spaceKey: string, target: WikiPage, views: number, inDialog?: () => Promise<unknown>) {
  await openCounted(page, spaceKey, target, views, async () => {
    await viewsButton(page).click();
    await expect(dialog(page).getByRole("heading", { name: "Page views" })).toBeVisible(ONE_LOOK);
    await inDialog?.();
  });
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

  test("bob reads a page and sees how often it was read; alice, who edits it, also sees that bob read it", async ({ page, api, pageAs, team }, testInfo) => {
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
    await expect(reader(page, team.people.bob.name)).toContainText("on 1 day");
    expect(await scrollsSideways(page)).toBe(false);
  });

  // Whether a name is shown belongs to the person, so somebody of the test's
  // own hides theirs: bob hiding his would hide him from the file's other tests.
  test("a reader hides their name in their profile, and alice sees them counted but not named", async ({ page, api, pageAs, makePerson }, testInfo) => {
    const { space, guide } = await readOnlySpace(api, testInfo);
    const person = await makePerson("reader");
    const them = await pageAs(person);
    await openViews(them, space.key, guide, 1);

    await them.goto("/settings/profile");
    const toggle = them.getByRole("switch", { name: "Show my name to the editors of pages I read" });
    await expect(toggle).toHaveAttribute("aria-checked", "true");
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-checked", "false");
    await expect(them.locator("[data-privacy-saved]")).toHaveText("Your choice is saved.");
    await expect(them.getByText("You are still counted when you read a page, only without your name.")).toBeVisible();
    expect(await scrollsSideways(them)).toBe(false);
    await them.context().close();

    await openViews(page, space.key, guide, 2, () =>
      expect(dialog(page).locator("[data-readers-unnamed]")).toHaveText("1 more person chose not to be named.", ONE_LOOK),
    );
    await expect(reader(page, person.name)).toHaveCount(0);
    await expect(dialog(page).locator("[data-reader]")).toHaveCount(1);
    await expect(dialog(page).locator("[data-views-in-all]")).toContainText("2 views");
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
    await openWithout(bob, pathOf(space.key, guide), bob.locator("main").filter({ hasText: guide.title }), bob.getByText(/not found/i).first());
    await expect(viewsButton(bob)).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the views open from the keyboard and are accessible in ${scheme}`, async ({ page, api }, testInfo) => {
      const { space, guide } = await readOnlySpace(api, testInfo);
      await startInScheme(page, scheme);
      await openCounted(page, space.key, guide, 1);
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
