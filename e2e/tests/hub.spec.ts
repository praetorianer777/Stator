import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const everywhere = (page: Page) => page.getByRole("navigation", { name: "Everywhere" }).first();

test.describe("the organization's hub", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  // The hub belongs to the organization a worker's tests share, so each test leaves none.
  test.afterEach(async ({ api }) => {
    must(await api.PUT("/org/hub", { body: { pageId: null, landing: false } }));
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  // The navigation is a drawer on a phone; landing there is the same redirect.
  test("an administrator chooses the hub, and a member lands on it with home a link away", { tag: "@desktop" }, async ({ page, api, pageAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    const space = await createSpace(api, key, uniqueName(testInfo, "News"));
    const front = await createPage(api, space.homePageId, "Front page");

    await page.goto("/settings/hub");
    await page.getByLabel("Space", { exact: true }).selectOption(key);
    await page.getByLabel("Page", { exact: true }).selectOption(front.id);
    await page.getByLabel("Everybody lands on the hub when they open Stator").check();
    await page.locator('[data-action="save-hub"]').click();
    await expect(page.locator("[data-hub-current]")).toContainText("Front page");

    const bob = await pageAs("bob");
    // Bob reads what alice chose, which a replica may not have yet.
    await expect(async () => {
      await bob.goto("/");
      await expect(heading(bob)).toHaveText("Front page", { timeout: 2_000 });
    }).toPass();
    await expect(bob).toHaveURL(new RegExp(`/p/${front.id}/front-page$`));
    await expect(everywhere(bob).getByRole("link", { name: "Hub" })).toBeVisible();
    await everywhere(bob).getByRole("link", { name: "Home" }).click();
    await expect(bob).toHaveURL(/\/home$/);
    await expect(bob.locator("[data-home]")).toBeVisible();
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the hub settings pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const key = uniqueKey(testInfo);
      made.push(key);
      const space = await createSpace(api, key, uniqueName(testInfo, "Axe"));
      const front = await createPage(api, space.homePageId, "Front page");
      must(await api.PUT("/org/hub", { body: { pageId: front.id, landing: false } }));
      await startInScheme(page, scheme);
      await page.goto("/settings/hub");
      await expect(page.locator("[data-hub-current]")).toContainText("Front page");
      await expectAccessible(page);
    });
  }
});
