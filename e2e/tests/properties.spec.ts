import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]");
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });
const properties = (rows: [string, string][]) => ({
  type: "properties",
  content: rows.map(([key, value]) => ({ type: "propertyRow", attrs: { key }, content: value ? [{ type: "text", text: value }] : undefined })),
});

async function label(api: StatorApi, pageId: string, name: string): Promise<void> {
  must(await api.POST("/pages/{pageID}/labels", { params: { path: { pageID: pageId } }, body: { name } }));
}

test.describe("page properties", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, title: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, title));
  }

  test("an author gives a page its properties, and a register of the labelled pages builds itself", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Registers");
    // Labels are the organization's, so each run takes its own.
    const tag = `adr-${space.key.toLowerCase()}`;
    const older = await createPage(api, space.homePageId, "Use Postgres", {
      type: "doc",
      content: [
        properties([
          ["Status", "Accepted"],
          ["Owner", "Ada"],
        ]),
      ],
    });
    await label(api, older.id, tag);
    const decision = await createPage(api, space.homePageId, "Drop the cache", { type: "doc", content: [paragraph("Why we drop it."), { type: "paragraph" }] });
    await label(api, decision.id, tag);

    await openShowing(page, `/s/${space.key}/p/${decision.id}/drop-the-cache/edit`, "Why we drop it.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/properties");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const names = editorBox(page).getByRole("textbox", { name: "Property name" });
    await expect(names).toHaveCount(2);
    // The caret waits in the first value, Owner's.
    await page.keyboard.type("Bob");
    await names.nth(1).fill("State");
    await editorBox(page).locator("td.doc-property-value").nth(1).click();
    await page.keyboard.type("Proposed");
    await page.keyboard.press("Enter");
    await expect(names.nth(2)).toBeFocused();
    await page.keyboard.type("Review date");
    await page.keyboard.press("Enter");
    await page.keyboard.type("2026-11-01");
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);
    const table = shown(page).locator("table[data-properties]");
    await expect(table.getByRole("rowheader")).toHaveText(["Owner", "State", "Review date"]);
    await expect(table.getByRole("row", { name: /State/ })).toContainText("Proposed");

    // The register lives on a page of its own and gathers both.
    const register = await createPage(api, space.homePageId, "Decision register", {
      type: "doc",
      content: [paragraph("All our decisions."), { type: "paragraph" }],
    });
    await openShowing(page, `/s/${space.key}/p/${register.id}/decision-register/edit`, "All our decisions.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/report");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Insert a properties report" });
    await dialog.getByLabel("Labels").fill(tag.toUpperCase());
    await dialog.getByLabel("Space").selectOption(space.key);
    await dialog.getByLabel("Properties to show").fill("Owner\nStatus");
    await dialog.getByRole("button", { name: "Insert" }).click();
    const inEditor = editorBox(page).locator("[data-properties-report-table]");
    await expect(inEditor.getByRole("rowheader")).toHaveText(["Drop the cache", "Use Postgres"]);
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const report = shown(page).locator("[data-properties-report-table]");
    await expect(report.getByRole("columnheader")).toHaveText([/^Page/, "Owner", "Status"]);
    await expect(report.getByRole("row", { name: /Use Postgres/ })).toContainText("Accepted");
    await report.getByRole("button", { name: "Owner" }).click();
    await report.getByRole("button", { name: "Owner" }).click();
    await expect(report.getByRole("rowheader")).toHaveText(["Drop the cache", "Use Postgres"]);
    await expect(report.getByRole("columnheader", { name: /Owner/ })).toHaveAttribute("aria-sort", "descending");
    await report.getByRole("link", { name: "Use Postgres" }).click();
    await expect(page).toHaveURL(new RegExp(`/p/${older.id}/`));
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`properties and a report pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const tag = `axe-${space.key.toLowerCase()}`;
      const one = await createPage(api, space.homePageId, "Release 1", {
        type: "doc",
        content: [
          properties([
            ["Owner", "Ada"],
            ["Status", "Shipped"],
            ["", ""],
          ]),
        ],
      });
      await label(api, one.id, tag);
      const register = await createPage(api, space.homePageId, "Releases", {
        type: "doc",
        content: [{ type: "propertiesReport", attrs: { labels: [tag], space: null, columns: [] } }],
      });
      await startInScheme(page, scheme);
      await openShowing(page, `/s/${space.key}/p/${register.id}/releases`, "Releases");
      await expect(shown(page).locator("[data-properties-report]")).toHaveAttribute("data-state", "report");
      await expectAccessible(page);
      await openShowing(page, `/s/${space.key}/p/${one.id}/release-1`, "Shipped");
      await page.screenshot({ path: testInfo.outputPath(`properties-${scheme}.png`), fullPage: true });
      await expectAccessible(page);
      await openShowing(page, `/s/${space.key}/p/${one.id}/release-1/edit`, "Shipped");
      await expect(editorBox(page).getByRole("textbox", { name: "Property name" })).toHaveCount(3);
      await page.screenshot({ path: testInfo.outputPath(`properties-edit-${scheme}.png`), fullPage: true });
      await expectAccessible(page);
    });
  }
});
