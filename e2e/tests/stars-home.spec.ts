import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openPage, openShowing, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

const list = (page: Page, name: string) => page.locator(`[data-home-list="${name}"]`);
const starPage = (page: Page) => page.locator('[data-action="star-page"]');
const starSpace = (page: Page) => page.locator('main [data-action="star-space"]');

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

type HomeList = "updates" | "starred" | "recent" | "edited";

/**
 * Opens the home page until every list named shows, or leaves out, a title in the same load. Each
 * list asks on its own, so a lagging replica can answer one before it has seen what another has.
 */
async function homeShows(page: Page, title: string, want: Partial<Record<HomeList, boolean>>) {
  await openUntil(page, "/", async () => {
    for (const [name, shown] of Object.entries(want)) {
      const link = list(page, name).getByRole("link", { name: title, exact: true });
      if (shown) await expect(link).toBeVisible(ONE_LOOK);
      else {
        // A list still loading shows no link either, so its answer is waited for first.
        await expect(list(page, name).locator("ul, p").first()).toBeVisible(ONE_LOOK);
        await expect(link).toHaveCount(0, ONE_LOOK);
      }
    }
  });
}

/** Publishes a new version of a page as whoever the client acts for. */
async function publishAs(api: StatorApi, target: WikiPage, text: string, comment: string) {
  const { page } = must(await api.GET("/pages/{pageID}", { params: { path: { pageID: target.id } } }));
  must(
    await api.PUT("/pages/{pageID}/draft", {
      params: { path: { pageID: target.id } },
      body: { title: target.title, body: doc(text), baseVersion: page.version },
    }),
  );
  must(await api.POST("/pages/{pageID}/publish", { params: { path: { pageID: target.id } }, body: { comment, notifyWatchers: false } }));
}

test.describe("stars and the home page", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("alice stars a page and a space, finds them on her home page with a colleague's update, and unstars one", async ({ page, api, apiAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Stars");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Plan"), doc("The plan."));
    const notes = await createPage(api, space.homePageId, uniqueName(testInfo, "Notes"), doc("Notes."));

    await openPage(page, space.key, plan);
    await expect(starPage(page)).toHaveAttribute("aria-pressed", "false");
    await starPage(page).click();
    await expect(starPage(page)).toHaveAttribute("aria-pressed", "true");

    await openShowing(page, `/s/${space.key}`, starSpace(page));
    await starSpace(page).click();
    await expect(starSpace(page)).toHaveAttribute("aria-pressed", "true");

    const bobApi = await apiAs("bob");
    const bob = must(await bobApi.GET("/auth/me")).user;
    await expect(async () => publishAs(bobApi, notes, "Bob's notes.", "Added the minutes")).toPass();

    await homeShows(page, notes.title, { updates: true });
    const update = list(page, "updates").locator("li").filter({ hasText: notes.title });
    await expect(update).toContainText(`${bob.name} published version 2`);
    await expect(update).toContainText("Added the minutes");
    await expect(list(page, "starred").getByRole("link", { name: plan.title, exact: true })).toBeVisible();
    await expect(list(page, "starred").getByRole("link", { name: space.name, exact: true })).toBeVisible();
    await expect(list(page, "recent").getByRole("link", { name: plan.title, exact: true })).toBeVisible();
    await expect(list(page, "edited").getByRole("link", { name: plan.title, exact: true })).toBeVisible();
    expect(await scrollsSideways(page)).toBe(false);

    await list(page, "starred")
      .getByRole("button", { name: `Unstar ${plan.title}` })
      .click();
    await expect(page.getByRole("status").filter({ hasText: `${plan.title} is no longer starred.` })).toBeAttached();
    await expect(list(page, "starred").getByRole("link", { name: plan.title, exact: true })).toHaveCount(0);
    await openPage(page, space.key, plan);
    await expect(starPage(page)).toHaveAttribute("aria-pressed", "false");
  });

  test("a page restricted away from bob leaves his stars and his updates", async ({ api, apiAs, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Hidden");
    const secret = await createPage(api, space.homePageId, uniqueName(testInfo, "Secret"), doc("Soon hidden."));
    const bobApi = await apiAs("bob");
    await expect.poll(async () => (await bobApi.PUT("/pages/{pageID}/star", { params: { path: { pageID: secret.id } } })).response.status).toBe(204);

    const bob = await pageAs("bob");
    await homeShows(bob, secret.title, { starred: true, updates: true });

    const alice = must(await api.GET("/auth/me")).user;
    must(
      await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: secret.id } }, body: { view: [{ type: "user", id: alice.id }], edit: [] } }),
    );
    await homeShows(bob, secret.title, { starred: false, updates: false });
    await expect.poll(async () => (await bobApi.PUT("/pages/{pageID}/star", { params: { path: { pageID: secret.id } } })).response.status).toBe(404);
  });

  test("a space is starred from the directory", { tag: "@desktop" }, async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Directory");
    const row = page.locator(`[data-space-row="${space.key}"]`);
    await openShowing(page, "/spaces", row);
    await row.locator('[data-action="star-space"]').click();
    await expect(row.locator('[data-action="star-space"]')).toHaveAttribute("aria-pressed", "true");
    await homeShows(page, space.name, { starred: true });
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`stars and the home page are accessible and keyboard usable in ${scheme}`, async ({ page, api, apiAs }, testInfo) => {
      const space = await freshSpace(api, testInfo, `Axe ${scheme}`);
      const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Checked"), doc("Words."));
      const bobApi = await apiAs("bob");
      await expect(async () => publishAs(bobApi, plan, "Bob was here.", "")).toPass();
      await startInScheme(page, scheme);

      await openPage(page, space.key, plan);
      await starPage(page).focus();
      await page.keyboard.press("Space");
      await expect(starPage(page)).toHaveAttribute("aria-pressed", "true");
      await expect(starPage(page)).toBeFocused();
      await expectAccessible(page);

      await homeShows(page, plan.title, { starred: true, updates: true });
      await expectAccessible(page);

      const all = page.getByRole("tab", { name: "All updates" });
      await all.focus();
      await page.keyboard.press("ArrowRight");
      const watched = page.getByRole("tab", { name: "Watched" });
      await expect(watched).toBeFocused();
      await expect(watched).toHaveAttribute("aria-selected", "true");
      // Alice made the page, so she watches it, and bob's version is among what she watches.
      await expect(list(page, "updates").getByRole("link", { name: plan.title, exact: true })).toBeVisible();
      await page.keyboard.press("ArrowLeft");
      await expect(all).toHaveAttribute("aria-selected", "true");
      await expect(list(page, "updates").getByRole("link", { name: plan.title, exact: true })).toBeVisible();

      const unstar = list(page, "starred").getByRole("button", { name: `Unstar ${plan.title}` });
      await unstar.focus();
      await page.keyboard.press("Enter");
      await expect(list(page, "starred").getByRole("link", { name: plan.title, exact: true })).toHaveCount(0);
      await expect(list(page, "starred").getByRole("heading", { name: "Starred" })).toBeFocused();
      await expectAccessible(page);
    });
  }
});
