import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openEditor, openUntil } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("[data-doc]");
const log = (page: Page) => page.locator("[data-decision-log]");

const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });
const decision = (state: string, value: string) => ({ type: "decision", attrs: { state }, content: [{ type: "text", text: value }] });

test.describe("decision items", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("one is marked in a page, decided with its button, and listed in the space's log", { tag: "@desktop" }, async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Decisions");
    // The empty paragraph at the end takes the block: an Enter after the caret
    // key would split wherever ProseMirror last saw the caret.
    const notes = await createPage(api, space.homePageId, "Release notes", { type: "doc", content: [paragraph("We met on Monday."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${notes.id}/release-notes`;

    await openEditor(page, path, "We met on Monday.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/decision");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    await page.keyboard.type("Ship on Tuesdays.");
    const toggle = editorBox(page).locator("[data-decision-toggle]");
    await expect(toggle).toHaveAttribute("aria-pressed", "false");
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-pressed", "true");
    await expect(toggle).toHaveText("Decided");
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    await expect(shown(page).locator('[data-decision="decided"]')).toContainText("DecidedShip on Tuesdays.");

    await page.locator(`[data-space-nav="${space.key}"]`).getByRole("link", { name: "Decisions" }).click();
    await expect(page).toHaveURL(new RegExp(`/s/${space.key}/decisions$`));
    await expect(log(page).locator('[data-decision-row="decided"]')).toContainText("Ship on Tuesdays.");
    await log(page).getByRole("button", { name: "Undecided" }).click();
    await expect(page).toHaveURL(/state=undecided$/);
    await expect(log(page).getByText("No decisions in this state.")).toBeVisible();
    await log(page).getByRole("button", { name: "All" }).click();
    await log(page)
      .getByRole("link", { name: /on Release notes/ })
      .click();
    await expect(page).toHaveURL(new RegExp(`/p/${notes.id}/release-notes$`));
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`decisions pass axe in ${scheme}, on a page and in the log`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const notes = await createPage(api, space.homePageId, "Notes", {
        type: "doc",
        content: [decision("decided", "Use Postgres."), decision("undecided", "Which region first.")],
      });
      await startInScheme(page, scheme);
      await openUntil(page, `/s/${space.key}/p/${notes.id}/notes`, () => expect(shown(page)).toContainText("Which region first.", ONE_LOOK));
      await expectAccessible(page);
      await page.goto(`/s/${space.key}/decisions`);
      await expect(log(page).locator("[data-decision-row]")).toHaveCount(2);
      await expectAccessible(page);
    });
  }
});
