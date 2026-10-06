import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openShowing, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]").first();

const HOURS = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71";
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });
const excerpt = (id: string, name: string, value: string) => ({ type: "excerpt", attrs: { id, name }, content: [paragraph(value)] });

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

    await openShowing(page, `${path}/edit`, "Welcome.");
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
    await openUntil(page, path, () => expect(hours).toContainText("Ten to six.", ONE_LOOK));

    const bob = await pageAs("bob");
    // Bob's reads are not held to the publish, so wait for words only it has.
    await openShowing(bob, path, "Ten to six.");
    await expect(bob.locator("main [data-doc]").first().getByRole("region", { name: "Included from Support: Hours" })).toContainText("Ten to six.");
    const notice = bob.locator('main [data-include][data-state="unavailable"]');
    await expect(notice).toContainText("This included content is not available to you");
    await expect(bob.locator("main")).not.toContainText("Salaries.");
  });

  test("a key typed before the browser reports the caret leaving an include goes after it, and the include is published", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Caret");
    const support = await createPage(api, space.homePageId, "Support", { type: "doc", content: [excerpt(HOURS, "Hours", "Nine to five.")] });
    const front = await createPage(api, space.homePageId, "Front", {
      type: "doc",
      content: [paragraph("Welcome."), { type: "include", attrs: { pageId: support.id, excerptId: HOURS } }, { type: "paragraph" }],
    });
    const path = `/s/${space.key}/p/${front.id}/front`;

    await openShowing(page, `${path}/edit`, "Nine to five.");
    await expect(page.locator("[data-page-editor]")).toHaveAttribute("data-collab", "together");
    const region = editorBox(page).getByRole("region", { name: "Included from Support: Hours" });
    await region.getByText("Nine to five.").click();
    await expect(editorBox(page).locator(".ProseMirror-selectednode")).toHaveCount(1);
    // End moves the caret at once and reports it in a selectionchange a busy
    // browser lets the next key overtake; the events here always overtake it.
    await editorBox(page).evaluate((box) => {
      document.getSelection()?.collapse(box.lastElementChild as Element, 0);
      box.dispatchEvent(new KeyboardEvent("keydown", { key: "/", bubbles: true, cancelable: true }));
      box.dispatchEvent(new KeyboardEvent("keypress", { key: "/", charCode: "/".charCodeAt(0), bubbles: true, cancelable: true }));
    });
    await expect(region).toContainText("Nine to five.");
    await page.keyboard.type("Thanks.");
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);
    await expect(shown(page)).toContainText("Thanks.");
    await expect(shown(page).getByRole("region", { name: "Included from Support: Hours" })).toContainText("Nine to five.");
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
      await openShowing(page, `/s/${space.key}/p/${front.id}/front`, "Nine to five.");
      await expect(page.locator('main [data-include][data-state="unavailable"]')).toHaveCount(1);
      await expectAccessible(page);
    });
  }
});
