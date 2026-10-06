import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { expectAccessible } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const ANONYMOUS = { cookies: [], origins: [] };
const BOB = "Bob Builder";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const dialog = (page: Page) => page.locator("[data-share-dialog]");
const links = (page: Page) => dialog(page).locator("[data-public-links]");

test.describe("public links", { tag: ["@auth"] }, () => {
  test("alice opens one page to anybody through a link, who reads it alone with nobody named, until she revokes it", async ({
    page,
    api,
    browser,
  }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const word = `linkword${Date.now().toString(36)}`;
    const space = await createSpace(api, key, "Linked handbook");
    const bob = must(await api.GET("/people", { params: { query: { q: "Bob" } } })).people.find((person) => person.name === BOB);
    expect(bob, "bob is a member to mention").toBeTruthy();
    const guide = await createPage(api, space.homePageId, `Guide ${word}`, {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: `Ask at the ${word} desk, or ask ` },
            { type: "mention", attrs: { id: bob!.id, label: BOB } },
          ],
        },
      ],
    });
    await createPage(api, guide.id, `Below ${word}`);
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    try {
      await expect(async () => {
        await page.goto(`/s/${key}/p/${guide.id}/guide`);
        await expect(heading(page)).toHaveText(guide.title, { timeout: 1_000 });
      }).toPass();
      await page.locator('[data-action="share-page"]').click();
      await links(page).getByRole("textbox", { name: "Label (optional)" }).fill("For the auditors");
      await links(page).getByRole("combobox", { name: "Runs out" }).selectOption("7");
      await links(page).locator('[data-action="create-public-link"]').click();
      const address = await links(page).locator("[data-public-link-address]").inputValue();
      expect(address).toMatch(/\/public\/[^/]+\/link\/[A-Za-z0-9_-]{43}$/);
      const listed = links(page).locator('[data-public-link="For the auditors"]');
      await expect(listed).toContainText("Made by you on");
      await expect(listed).toContainText("Runs out on");
      await expectAccessible(page);

      // The reader is not alice, so a replica may not have her link yet.
      const reader = await context.newPage();
      await expect(async () => {
        await reader.goto(address);
        await expect(heading(reader)).toHaveText(guide.title, { timeout: 2_000 });
      }).toPass();
      const shell = await context.request.get(address);
      expect(shell.headers()["cache-control"]).toBe("no-store");
      expect(shell.headers()["referrer-policy"]).toBe("no-referrer");
      await expect(reader.locator("main")).toContainText(`Ask at the ${word} desk`);
      await expect(reader.locator("main [data-mention]")).toHaveText("@someone");
      await expect(reader.locator("main")).not.toContainText(BOB);
      await expect(reader.locator('meta[name="robots"]')).toHaveAttribute("content", "noindex, nofollow");
      await expect(reader.locator("[data-public-tree], [data-public-search], [data-rail], [data-sidebar], [data-comments]")).toHaveCount(0);
      await expect(reader.getByText(`Below ${word}`)).toHaveCount(0);
      await expectAccessible(reader);

      await listed.getByRole("button", { name: "Revoke For the auditors" }).click();
      await expect(listed).toHaveCount(0);
      await expect(async () => {
        await reader.goto(address);
        await expect(reader.locator("[data-link-gone]")).toBeVisible({ timeout: 2_000 });
      }).toPass();
      await expect(reader.locator("[data-link-gone]")).toContainText("This link does not work any more");
      await expect(reader.locator("main")).not.toContainText(word);
    } finally {
      await context.close();
      await deleteSpace(api, key);
    }
  });

  test("an administrator stops every link and allows them again, and the links come back", async ({ page, api, browser }, testInfo) => {
    test.slow();
    const key = uniqueKey(testInfo);
    const space = await createSpace(api, key, "Stopped links");
    const plan = await createPage(api, space.homePageId, `Plan ${key}`, {
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "The plan in words." }] }],
    });
    const made = must(await api.POST("/pages/{pageID}/public-links", { params: { path: { pageID: plan.id } }, body: {} }));
    const context = await browser.newContext({ baseURL: WEB_URL, storageState: ANONYMOUS });
    const reader = await context.newPage();
    const settings = page.locator("[data-public-links-settings]");
    try {
      await page.goto("/settings/permissions");
      await settings.locator('[data-action="public-links"]').click();
      await expect(settings.getByRole("status")).toHaveText("Saved.");
      await expect(async () => {
        await reader.goto(made.path);
        await expect(reader.locator("[data-link-gone]")).toBeVisible({ timeout: 2_000 });
      }).toPass();

      await expect(async () => {
        await page.goto(`/s/${key}/p/${plan.id}/plan`);
        await expect(heading(page)).toHaveText(plan.title, { timeout: 1_000 });
      }).toPass();
      await page.locator('[data-action="share-page"]').click();
      await expect(links(page).locator('[data-public-links-refusal="off"]')).toContainText("Your organization does not allow public links.");
      await expect(links(page).locator('[data-action="create-public-link"]')).toHaveCount(0);

      await page.goto("/settings/permissions");
      await settings.locator('[data-action="public-links"]').click();
      await expect(settings.getByRole("status")).toHaveText("Saved.");
      await expect(async () => {
        await reader.goto(made.path);
        await expect(heading(reader)).toHaveText(plan.title, { timeout: 2_000 });
      }).toPass();
    } finally {
      await context.close();
      must(await api.PUT("/org/public-links", { body: { enabled: true } }));
      await deleteSpace(api, key);
    }
  });
});
