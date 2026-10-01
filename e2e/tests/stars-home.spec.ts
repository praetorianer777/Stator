import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const list = (page: Page, name: string) => page.locator(`[data-home-list="${name}"]`);
const starPage = (page: Page) => page.locator('[data-action="star-page"]');
const starSpace = (page: Page) => page.locator('main [data-action="star-space"]');

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** Opens a page by id, waiting out a replica that has not seen it yet. */
async function openPage(page: Page, spaceKey: string, target: WikiPage) {
  await expect(async () => {
    await page.goto(`/s/${spaceKey}/p/${target.id}/page`);
    await expect(heading(page)).toHaveText(target.title, { timeout: 1_000 });
  }).toPass();
}

/** Opens the home page until a list shows what a test waits for, which a replica may lag behind. */
async function homeShows(page: Page, listName: string, title: string, shown = true) {
  await expect(async () => {
    await page.goto("/");
    const link = list(page, listName).getByRole("link", { name: title, exact: true });
    if (shown) await expect(link).toBeVisible({ timeout: 1_000 });
    else {
      await expect(list(page, listName).locator("ul, p").first()).toBeVisible({ timeout: 1_000 });
      await expect(link).toHaveCount(0, { timeout: 1_000 });
    }
  }).toPass();
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

    await expect(async () => {
      await page.goto(`/s/${space.key}`);
      await expect(starSpace(page)).toBeVisible({ timeout: 1_000 });
    }).toPass();
    await starSpace(page).click();
    await expect(starSpace(page)).toHaveAttribute("aria-pressed", "true");

    const bobApi = await apiAs("bob");
    const bob = must(await bobApi.GET("/auth/me")).user;
    await expect(async () => publishAs(bobApi, notes, "Bob's notes.", "Added the minutes")).toPass();

    await homeShows(page, "updates", notes.title);
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
    await homeShows(bob, "starred", secret.title);
    await expect(list(bob, "updates").getByRole("link", { name: secret.title, exact: true })).toBeVisible();

    const alice = must(await api.GET("/auth/me")).user;
    must(
      await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: secret.id } }, body: { view: [{ type: "user", id: alice.id }], edit: [] } }),
    );
    await homeShows(bob, "starred", secret.title, false);
    await expect(list(bob, "updates").getByRole("link", { name: secret.title, exact: true })).toHaveCount(0);
    await expect.poll(async () => (await bobApi.PUT("/pages/{pageID}/star", { params: { path: { pageID: secret.id } } })).response.status).toBe(404);
  });

  test("a space is starred from the directory", { tag: "@desktop" }, async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Directory");
    const row = page.locator(`[data-space-row="${space.key}"]`);
    await expect(async () => {
      await page.goto("/spaces");
      await expect(row).toBeVisible({ timeout: 1_000 });
    }).toPass();
    await row.locator('[data-action="star-space"]').click();
    await expect(row.locator('[data-action="star-space"]')).toHaveAttribute("aria-pressed", "true");
    await homeShows(page, "starred", space.name);
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

      await homeShows(page, "starred", plan.title);
      await expect(list(page, "updates").getByRole("link", { name: plan.title, exact: true })).toBeVisible();
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
