import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { openEditor, openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("[data-doc]");

const text = (value: string) => [{ type: "text", text: value }];
const paragraph = (value: string) => ({ type: "paragraph", content: text(value) });

async function insert(page: Page, query: string): Promise<void> {
  await page.keyboard.type(`/${query}`);
  await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
  await page.keyboard.press("Enter");
}

test.describe("expand blocks", { tag: ["@auth", "@desktop"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("one goes in from the slash menu with a title, and reads closed until opened, open in print", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Expand");
    // The empty paragraph at the end takes the block: an Enter after the caret
    // key would split wherever ProseMirror last saw the caret.
    const runbook = await createPage(api, space.homePageId, "Runbook", { type: "doc", content: [paragraph("Deploy on Tuesdays."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${runbook.id}/runbook`;

    await openEditor(page, path, "Deploy on Tuesdays.");
    await caretTo(editorBox(page), "end");
    await insert(page, "expand");
    const title = editorBox(page).getByRole("textbox", { name: "Expand title" });
    await expect(title).toBeFocused();
    await page.keyboard.type("Rollback steps");
    await page.keyboard.press("Enter");
    await page.keyboard.type("Revert the release, then tell the team.");
    await expect(editorBox(page).locator("[data-expand] [data-expand-body]")).toContainText("Revert the release");
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const toggle = shown(page).getByRole("button", { name: "Rollback steps", exact: true });
    const body = shown(page).getByText("Revert the release, then tell the team.");
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await expect(body).toBeHidden();
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    await expect(body).toBeVisible();
    await toggle.press("Enter");
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await expect(body).toBeHidden();
    await toggle.press("Space");
    await expect(body).toBeVisible();

    // Whether it is open is the reader's own, so a fresh visit starts closed.
    await page.reload();
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await expect(body).toBeHidden();
    await page.emulateMedia({ media: "print" });
    await expect(body).toBeVisible();
    await page.emulateMedia({ media: "screen" });
    await expect(body).toBeHidden();

    await openEditor(page, path, "Revert the release");
    await expect(editorBox(page).getByRole("textbox", { name: "Expand title" })).toHaveValue("Rollback steps");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`an expand block passes axe in ${scheme}, closed, open and in the editor`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const notes = await createPage(api, space.homePageId, "Notes", {
        type: "doc",
        content: [
          paragraph("The short version."),
          { type: "expand", attrs: { title: "The long version" }, content: [paragraph("Every detail, for whoever wants it.")] },
          { type: "expand", attrs: { title: "" }, content: [paragraph("Untitled detail.")] },
        ],
      });
      await startInScheme(page, scheme);
      const path = `/s/${space.key}/p/${notes.id}/notes`;

      await openShowing(page, path, shown(page).getByRole("button", { name: "The long version" }));
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expect(shown(page).getByRole("button", { name: "Details" })).toHaveAttribute("aria-expanded", "false");
      await expectAccessible(page);

      const toggle = shown(page).getByRole("button", { name: "The long version" });
      await toggle.focus();
      await page.keyboard.press("Enter");
      await expect(shown(page).getByText("Every detail, for whoever wants it.")).toBeVisible();
      await expectAccessible(page);

      await openEditor(page, path, "Every detail");
      await expect(editorBox(page).getByRole("textbox", { name: "Expand title" }).first()).toHaveValue("The long version");
      await expectAccessible(page);
    });
  }
});
