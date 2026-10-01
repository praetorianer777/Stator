import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Page as WikiPage } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const pageBar = (page: Page) => page.getByRole("list", { name: "Reactions to this page" });
const toggle = (page: Page, emoji: string) => pageBar(page).locator(`[data-reaction="${emoji}"]`);

const doc = (text: string) => ({ type: "doc" as const, content: [{ type: "paragraph", content: [{ type: "text", text }] }] });

/** Opens a page by id, waiting out a replica that has not seen it yet. */
async function openPage(page: Page, spaceKey: string, target: WikiPage) {
  await expect(async () => {
    await page.goto(`/s/${spaceKey}/p/${target.id}/page`);
    await expect(heading(page)).toHaveText(target.title, { timeout: 1_000 });
  }).toPass();
}

test.describe("reactions", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("alice and bob react to a page and a comment, see the counts and who reacted, and take theirs back", async ({ page, api, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "React");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Plan"), doc("The plan."));
    const started = must(await api.POST("/pages/{pageID}/comments", { params: { path: { pageID: plan.id } }, body: { body: doc("Ship on Friday?") } })).thread;

    await openPage(page, space.key, plan);
    await pageBar(page).locator('[data-action="add-reaction"]').click();
    await page.locator('[data-reaction-choice="🎉"]').click();
    await expect(toggle(page, "🎉")).toHaveAttribute("aria-pressed", "true");
    await expect(toggle(page, "🎉").locator("[data-reaction-count]")).toHaveText("1");

    const commentBar = page.locator(`[data-comment="${started.id}"] [data-reactions]`);
    await commentBar.locator('[data-action="add-reaction"]').click();
    await page.locator('[data-reaction-choice="👍"]').click();
    await expect(commentBar.locator('[data-reaction="👍"]')).toHaveAttribute("aria-pressed", "true");

    await pageBar(page).locator('[data-action="add-reaction"]').click();
    await page.locator('[data-action="more-reactions"]').click();
    const search = page.locator("[data-reaction-search]");
    await search.locator("[data-reaction-search-input]").fill("taco");
    await search.locator('[data-reaction-search-option="🌮"]').click();
    await expect(search).toHaveCount(0);
    await expect(toggle(page, "🌮")).toHaveAttribute("aria-pressed", "true");

    const bob = await pageAs("bob");
    await expect(async () => {
      await openPage(bob, space.key, plan);
      await expect(toggle(bob, "🎉")).toBeVisible({ timeout: 1_000 });
    }).toPass();
    await expect(toggle(bob, "🎉")).toHaveAttribute("aria-pressed", "false");
    await toggle(bob, "🎉").click();
    await expect(toggle(bob, "🎉").locator("[data-reaction-count]")).toHaveText("2");
    await expect(toggle(bob, "🎉")).toHaveAttribute("aria-pressed", "true");
    await toggle(bob, "🎉").hover();
    await expect(bob.getByRole("tooltip")).toHaveText(/^You and .+ reacted with 🎉$/);
    await expect(bob.locator(`[data-comment="${started.id}"] [data-reaction="👍"] [data-reaction-count]`)).toHaveText("1");

    await expect(async () => {
      await page.reload();
      await expect(toggle(page, "🎉").locator("[data-reaction-count]")).toHaveText("2", { timeout: 1_000 });
    }).toPass();
    await toggle(page, "🎉").hover();
    await expect(page.getByRole("tooltip")).toHaveText(/^You and .+ reacted with 🎉$/);
    await toggle(page, "🎉").click();
    await expect(toggle(page, "🎉").locator("[data-reaction-count]")).toHaveText("1");
    await expect(toggle(page, "🎉")).toHaveAttribute("aria-pressed", "false");
    await expect(toggle(page, "🎉").locator("[data-reaction-who]")).not.toContainText("You");

    await commentBar.locator('[data-reaction="👍"]').click();
    await expect(commentBar.locator('[data-reaction="👍"]')).toHaveCount(0);
  });

  test("a member who may only read sees who reacted and cannot react", async ({ api, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Read");
    const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Read only"), doc("Read me."));
    must(await api.POST("/pages/{pageID}/reactions", { params: { path: { pageID: plan.id } }, body: { emoji: "👀" } }));
    must(
      await api.PUT("/spaces/{spaceKey}/permissions", {
        params: { path: { spaceKey: space.key } },
        body: { grants: [{ subject: { type: "everyone" }, permissions: ["view"] }] },
      }),
    );

    const bob = await pageAs("bob");
    await expect(async () => {
      await openPage(bob, space.key, plan);
      await expect(toggle(bob, "👀")).toHaveAttribute("aria-disabled", "true", { timeout: 1_000 });
    }).toPass();
    await expect(pageBar(bob).locator('[data-action="add-reaction"]')).toHaveCount(0);
    await toggle(bob, "👀").focus();
    await expect(bob.getByRole("tooltip")).toHaveText(/^.+ reacted with 👀$/);
    await bob.keyboard.press("Enter");
    await expect(toggle(bob, "👀").locator("[data-reaction-count]")).toHaveText("1");
    await expect(toggle(bob, "👀")).toHaveAttribute("aria-pressed", "false");
  });

  test("somebody who may not view a page neither sees nor makes its reactions", async ({ api, apiAs, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Hidden");
    const secret = await createPage(api, space.homePageId, uniqueName(testInfo, "Secret"), doc("Hidden."));
    must(await api.POST("/pages/{pageID}/reactions", { params: { path: { pageID: secret.id } }, body: { emoji: "🚀" } }));
    const alice = must(await api.GET("/auth/me")).user;
    must(
      await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: secret.id } }, body: { view: [{ type: "user", id: alice.id }], edit: [] } }),
    );

    const bobApi = await apiAs("bob");
    await expect
      .poll(async () => (await bobApi.POST("/pages/{pageID}/reactions", { params: { path: { pageID: secret.id } }, body: { emoji: "🚀" } })).response.status)
      .toBe(404);
    expect((await bobApi.DELETE("/pages/{pageID}/reactions", { params: { path: { pageID: secret.id }, query: { emoji: "🚀" } } })).response.status).toBe(404);

    const bob = await pageAs("bob");
    await bob.goto(`/s/${space.key}/p/${secret.id}/secret`);
    await expect(bob.getByText(/not found/i).first()).toBeVisible();
    await expect(pageBar(bob)).toHaveCount(0);
    await expect(bob.getByText("🚀")).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`reactions are accessible and keyboard usable in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, `Axe ${scheme}`);
      const plan = await createPage(api, space.homePageId, uniqueName(testInfo, "Checked"), doc("Words."));
      must(await api.POST("/pages/{pageID}/reactions", { params: { path: { pageID: plan.id } }, body: { emoji: "👍" } }));
      await startInScheme(page, scheme);
      await expect(async () => {
        await openPage(page, space.key, plan);
        await expect(toggle(page, "👍")).toBeVisible({ timeout: 1_000 });
      }).toPass();
      await expectAccessible(page);

      const thumbs = toggle(page, "👍");
      await thumbs.focus();
      await expect(page.getByRole("tooltip")).toHaveText("You reacted with 👍");
      await page.keyboard.press("Enter");
      await expect(thumbs).toHaveCount(0);

      const add = pageBar(page).locator('[data-action="add-reaction"]');
      await add.focus();
      await page.keyboard.press("Enter");
      const menu = page.getByRole("menu", { name: "Pick a reaction" });
      await expect(menu.getByRole("menuitem").first()).toBeFocused();
      await expectAccessible(page);
      await page.keyboard.press("ArrowDown");
      await page.keyboard.press("Enter");
      await expect(menu).toHaveCount(0);
      await expect(add).toBeFocused();
      await expect(pageBar(page).locator("[data-reaction][aria-pressed=true]")).toHaveCount(1);
      await page.keyboard.press("Shift+Tab");
      await expect(pageBar(page).locator("[data-reaction]").first()).toBeFocused();
      await page.keyboard.press("Space");
      await expect(pageBar(page).locator("[data-reaction]")).toHaveCount(0);
      await expectAccessible(page);
    });
  }
});
