import type { Page } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { childTitles, createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey } from "../fixtures/spaces";

const tree = (page: Page) => page.locator("[data-page-tree]");
const item = (page: Page, name: string) => tree(page).getByRole("treeitem", { name, exact: true });
const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const children = (page: Page) => page.locator("[data-folder-children]");

async function menu(page: Page, action: string): Promise<void> {
  await page.locator('[data-action="page-menu"]').click();
  await page.locator(`[role="menu"] [data-action="${action}"]`).click();
}

async function createFolder(api: StatorApi, parentId: string, title: string) {
  return must(await api.POST("/pages", { body: { parentId, title, kind: "folder" } })).page;
}

test.describe("folders", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: Parameters<typeof uniqueKey>[0], label: string) {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("a folder is made, filled, renamed, moved and deleted from the tree's pages", { tag: "@desktop" }, async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Folders");
    const archive = await createPage(api, space.homePageId, "Archive");
    await page.goto(`/s/${space.key}`);

    await menu(page, "new-folder");
    const made = page.getByRole("dialog");
    await made.getByLabel("Title", { exact: true }).fill("Guides");
    await page.locator('[data-action="confirm-new-page"]').click();
    await expect(page).toHaveURL(/\/p\/[0-9a-f-]+\/guides$/);
    await expect(heading(page)).toHaveText("Guides");
    await expect(item(page, "Guides, folder")).toHaveAttribute("aria-selected", "true");
    await expect(page.locator('[data-action="edit-page"]')).toHaveCount(0);

    await page.locator('[data-action="new-page"]').click();
    await page.getByRole("dialog").getByLabel("Title", { exact: true }).fill("Install");
    await page.locator('[data-action="confirm-new-page"]').click();
    await page.locator("#page-body").click();
    await page.keyboard.type("Run the installer.");
    await publishFromEditor(page);
    await expect(item(page, "Install")).toHaveAttribute("aria-level", "2");

    await item(page, "Guides, folder").getByRole("link", { name: "Guides", exact: true }).click();
    await expect(children(page).getByRole("link", { name: "Install" })).toBeVisible();

    await menu(page, "rename-folder");
    const rename = page.getByRole("dialog", { name: "Rename Guides" });
    await rename.getByLabel("Title", { exact: true }).fill("How-tos");
    await rename.locator('[data-action="confirm-rename"]').click();
    await expect(heading(page)).toHaveText("How-tos");
    await expect(item(page, "How-tos, folder")).toBeVisible();

    await menu(page, "move-page");
    const move = page.getByRole("dialog", { name: "Move How-tos" });
    await move.getByLabel("Put it under", { exact: true }).selectOption(archive.id);
    await move.locator('[data-action="confirm-move"]').click();
    await expect(item(page, "How-tos, folder")).toHaveAttribute("aria-level", "2");
    expect(await childTitles(api, space.key, archive.id)).toEqual(["How-tos"]);

    page.on("dialog", (dialog) => void dialog.accept());
    await menu(page, "trash-page");
    await expect(page).toHaveURL(new RegExp(`/p/${archive.id}/archive$`));
    expect(await childTitles(api, space.key, archive.id)).toEqual([]);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`a folder and the tree beside it pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const box = await createFolder(api, space.homePageId, "Box");
      await createFolder(api, box.id, "Inner");
      await createPage(api, box.id, "Note");
      await startInScheme(page, scheme);
      await page.goto(`/s/${space.key}/p/${box.id}/box`);
      await expect(children(page).getByRole("link", { name: "Note" })).toBeVisible();
      await expectAccessible(page);
    });
  }
});
