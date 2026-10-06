import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openShowing, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const doc = (...lines: string[]) => ({ type: "doc" as const, content: lines.map((text) => ({ type: "paragraph", content: [{ type: "text", text }] })) });
const bell = (page: Page) => page.locator('[data-action="notifications"]');
const panel = (page: Page) => page.locator("[data-notification-panel]");
const listed = (page: Page) => page.locator("main [data-doc] [data-blog-posts] [data-listed-post]");

/** The month a post published now is filed under, in UTC as the blog files it. */
const thisMonth = () => new Date().toISOString().slice(0, 7);

/** Writes a post through the API, published at once, as whoever the client is. */
async function post(api: StatorApi, space: Space, title: string, text: string) {
  return must(await api.POST("/spaces/{spaceKey}/posts", { params: { path: { spaceKey: space.key } }, body: { title, body: doc(text), publish: true } })).page;
}

test.describe("blog posts", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, title: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, title));
  }

  test(
    "alice writes a post in her space's blog, it is filed under its month, bob who watches the blog hears, and a block lists it for him",
    { tag: ["@desktop"] },
    async ({ page, api, pageAs }, testInfo) => {
      const space = await freshSpace(api, testInfo, "News");
      const blogPath = `/s/${space.key}/blog`;

      const bob = await pageAs("bob");
      await openUntil(bob, blogPath, () => expect(bob.locator('[data-action="watch-blog"]')).toBeVisible(ONE_LOOK));
      await bob.locator('[data-action="watch-blog"]').click();
      await expect(bob.locator('[data-action="watch-blog"]')).toHaveAttribute("data-watching", "true");

      await page.goto(blogPath);
      await page.locator('[data-action="new-post"]').click();
      const dialog = page.locator("[data-new-post-dialog]");
      await dialog.getByLabel("Title").fill("Launch day");
      await dialog.locator('[data-action="create-post"]').click();
      await page.waitForURL((url) => url.pathname.endsWith("/edit"));
      await caretTo(page.locator("#page-body"), "end");
      await page.keyboard.type("We ship on Monday.");
      await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
      await publishFromEditor(page);
      await expect(page.locator("[data-post-date]")).toContainText("Posted on");
      await expect(page.locator("[data-blog-crumb]")).toBeVisible();

      await page.goto(blogPath);
      await expect(page.locator("[data-post-title]")).toHaveAttribute("data-post-title", "Launch day");
      await expect(page.locator(`[data-blog-month="${thisMonth()}"]`)).toHaveAttribute("data-count", "1");
      await page.locator(`[data-blog-month="${thisMonth()}"] a`).click();
      await expect(page).toHaveURL(/month=/);
      await expect(page.locator("[data-post-title]")).toHaveAttribute("data-post-title", "Launch day");
      expect(await scrollsSideways(page)).toBe(false);

      const told = panel(bob).locator('[data-notification="posted"]');
      await expect(async () => {
        await bob.goto("/");
        await bell(bob).click();
        await expect(told).toContainText("posted Launch day", ONE_LOOK);
      }).toPass();

      const overview = await createPage(api, space.homePageId, "Overview", {
        type: "doc",
        content: [{ type: "blogPosts", attrs: { space: space.key, limit: 5 } }],
      });
      const overviewPath = `/s/${space.key}/p/${overview.id}/overview`;
      await openShowing(bob, overviewPath, bob.locator('main [data-doc] [data-listed-post="Launch day"]'));

      // A post closed to bob stays out of his list. The post after it proves
      // his read has replayed the restriction, which came before it.
      const secret = await post(api, space, "Secret plans", "Only for alice.");
      const alice = must(await api.GET("/auth/me")).user;
      must(
        await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: secret.id } }, body: { view: [{ type: "user", id: alice.id }], edit: [] } }),
      );
      await post(api, space, "Doors at nine", "See you there.");
      await openShowing(bob, overviewPath, bob.locator('main [data-doc] [data-listed-post="Doors at nine"]'));
      await expect(listed(bob)).toHaveText([/Doors at nine/, /Launch day/]);

      await page.goto(overviewPath);
      await expect(listed(page)).toHaveText([/Doors at nine/, /Secret plans/, /Launch day/]);
    },
  );

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`a blog passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, `Axe ${scheme}`);
      await post(api, space, "Hello", "The first post.");
      await startInScheme(page, scheme);
      await openShowing(page, `/s/${space.key}/blog`, page.locator('[data-post-title="Hello"]'));
      await expect(page.locator("[data-blog-dates]")).toBeVisible();
      await expectAccessible(page);
      expect(await scrollsSideways(page)).toBe(false);
    });
  }
});
