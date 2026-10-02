import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]").first();

const HOURS = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71";
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });
const excerpt = (id: string, name: string, value: string) => ({ type: "excerpt", attrs: { id, name }, content: [paragraph(value)] });

/** Opens a page until it holds the words given, which a replica may lag behind on. */
async function open(page: Page, path: string, words: string): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await expect(page.locator("main")).toContainText(words, { timeout: 2_000 });
  }).toPass();
}

test.describe("includes", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("an author includes another page's excerpt, which follows that page, and a reader without access sees a notice", async ({
    page,
    api,
    pageAs,
  }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Includes");
    const support = await createPage(api, space.homePageId, "Support", {
      type: "doc",
      content: [paragraph("About support."), excerpt(HOURS, "Hours", "Nine to five.")],
    });
    const secret = await createPage(api, space.homePageId, "Secret", { type: "doc", content: [paragraph("Salaries.")] });
    const me = must(await api.GET("/auth/me")).user.id;
    must(await api.PUT("/pages/{pageID}/restrictions", { params: { path: { pageID: secret.id } }, body: { view: [{ type: "user", id: me }], edit: [] } }));
    const front = await createPage(api, space.homePageId, "Front", { type: "doc", content: [paragraph("Welcome."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${front.id}/front`;

    await open(page, `${path}/edit`, "Welcome.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/include");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const picker = page.getByRole("dialog", { name: "Choose what to include" });
    // The page being edited cannot include itself, so it is not offered.
    await expect(picker.getByLabel("Page", { exact: true }).locator("option", { hasText: "Front" })).toHaveCount(0);
    await picker.getByLabel("Page", { exact: true }).selectOption(support.id);
    await picker.getByRole("radio", { name: /Hours/ }).check();
    await picker.getByRole("button", { name: "Include" }).click();
    await expect(editorBox(page).getByRole("region", { name: "Included from Support: Hours" })).toContainText("Nine to five.");

    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/include");
    await page.keyboard.press("Enter");
    await picker.getByLabel("Page", { exact: true }).selectOption(secret.id);
    await picker.getByRole("button", { name: "Include" }).click();
    await expect(editorBox(page).getByRole("region", { name: "Included from Secret" })).toContainText("Salaries.");

    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);
    const hours = shown(page).getByRole("region", { name: "Included from Support: Hours" });
    await expect(hours).toContainText("Nine to five.");
    await expect(hours).not.toContainText("About support.");

    // The include follows the page it comes from.
    must(
      await api.PATCH("/pages/{pageID}", {
        params: { path: { pageID: support.id } },
        body: { version: 1, body: { type: "doc", content: [paragraph("About support."), excerpt(HOURS, "Hours", "Ten to six.")] } },
      }),
    );
    await expect(async () => {
      await page.reload();
      await expect(hours).toContainText("Ten to six.", { timeout: 2_000 });
    }).toPass();

    const bob = await pageAs("bob");
    await open(bob, path, "Welcome.");
    await expect(bob.locator("main [data-doc]").first().getByRole("region", { name: "Included from Support: Hours" })).toContainText("Ten to six.");
    const notice = bob.locator('main [data-include][data-state="unavailable"]');
    await expect(notice).toContainText("This included content is not available to you");
    await expect(bob.locator("main")).not.toContainText("Salaries.");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`includes pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const support = await createPage(api, space.homePageId, "Support", { type: "doc", content: [excerpt(HOURS, "Hours", "Nine to five.")] });
      const front = await createPage(api, space.homePageId, "Front", {
        type: "doc",
        content: [
          { type: "include", attrs: { pageId: support.id, excerptId: HOURS } },
          { type: "include", attrs: { pageId: "0195f000-0000-7000-8000-000000000999", excerptId: null } },
        ],
      });
      await startInScheme(page, scheme);
      await open(page, `/s/${space.key}/p/${front.id}/front`, "Nine to five.");
      await expect(page.locator('main [data-include][data-state="unavailable"]')).toHaveCount(1);
      await expectAccessible(page);
    });
  }
});
