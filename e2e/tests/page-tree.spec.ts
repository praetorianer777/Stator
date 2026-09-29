import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { childTitles, createPage, createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const tree = (page: Page) => page.locator("[data-page-tree]");
const item = (page: Page, title: string) => tree(page).getByRole("treeitem", { name: title, exact: true });
const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });

test.describe("the page tree", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: Parameters<typeof createSpace>[0], testInfo: Parameters<typeof uniqueKey>[0], label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("child pages are created from the page they go under", { tag: "@desktop" }, async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Children");
    await page.goto(`/s/${space.key}`);

    await page.locator('[data-action="new-page"]').click();
    await page.getByRole("dialog").getByLabel("Title", { exact: true }).fill("Onboarding");
    await page.locator('[data-action="confirm-new-page"]').click();
    await expect(page).toHaveURL(/\/p\/[0-9a-f-]+\/onboarding\/edit$/);
    await page.locator("#page-body").click();
    await page.keyboard.type("Start here.");
    await page.locator('[data-action="save-page"]').click();
    await expect(page).toHaveURL(/\/p\/[0-9a-f-]+\/onboarding$/);
    await expect(item(page, "Onboarding")).toHaveAttribute("aria-selected", "true");

    await page.locator('[data-action="new-page"]').click();
    await page.getByRole("dialog").getByLabel("Title", { exact: true }).fill("First week");
    await page.locator('[data-action="confirm-new-page"]').click();
    await page.locator('[data-action="save-page"]').click();
    await expect(heading(page)).toHaveText("First week");
    await expect(item(page, "First week")).toHaveAttribute("aria-level", "2");
    await expect(page.getByRole("navigation", { name: "Breadcrumb" }).getByRole("link", { name: "Onboarding" })).toBeVisible();
  });

  test("a page dragged onto another goes under it", { tag: "@desktop" }, async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Drag");
    const target = await createPage(api, space.homePageId, "Target");
    await createPage(api, space.homePageId, "Dragged");
    await page.goto(`/s/${space.key}`);

    const row = item(page, "Target").locator("[data-tree-row]");
    await item(page, "Dragged").dragTo(row);
    await expect(page.locator("[data-tree-status]")).toHaveText("Moved Dragged.");
    await expect(item(page, "Dragged")).toHaveAttribute("aria-level", "2");
    expect(await childTitles(api, space.key, target.id)).toEqual(["Dragged"]);

    // A page cannot go under a page below it; the tree does not even take the drop.
    await item(page, "Target").dragTo(item(page, "Dragged").locator("[data-tree-row]"));
    expect(await childTitles(api, space.key, space.homePageId)).toEqual(["Target"]);
  });

  test("a page is moved with the keyboard through the move dialog", { tag: "@desktop" }, async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Keys");
    const parent = await createPage(api, space.homePageId, "Parent");
    await createPage(api, space.homePageId, "Mover");
    await page.goto(`/s/${space.key}`);

    await item(page, "Parent").focus();
    await page.keyboard.press("ArrowDown");
    await expect(item(page, "Mover")).toBeFocused();
    await page.keyboard.press("m");
    const dialog = page.getByRole("dialog", { name: "Move Mover" });
    await expect(dialog).toBeVisible();
    await dialog.getByLabel("Put it under", { exact: true }).selectOption(parent.id);
    await dialog.locator('[data-action="confirm-move"]').press("Enter");

    await expect(page).toHaveURL(/\/p\/[0-9a-f-]+\/mover$/);
    await expect(item(page, "Mover")).toHaveAttribute("aria-level", "2");
    expect(await childTitles(api, space.key, parent.id)).toEqual(["Mover"]);
  });

  test("a page and the tree beside it pass axe", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Axe");
    const parent = await createPage(api, space.homePageId, "Parent");
    const child = await createPage(api, parent.id, "Child");
    await page.goto(`/s/${space.key}/p/${child.id}/child`);
    await expect(heading(page)).toHaveText("Child");
    await expectAccessible(page);
    if (await tree(page).isVisible()) {
      await expect(item(page, "Child")).toHaveAttribute("aria-selected", "true");
    }
  });
});
