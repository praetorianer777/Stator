import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]");

function table(...rows: string[][]) {
  return {
    type: "table",
    content: rows.map((row, r) => ({
      type: "tableRow",
      content: row.map((text) => ({ type: r === 0 ? "tableHeader" : "tableCell", content: [{ type: "paragraph", content: [{ type: "text", text }] }] })),
    })),
  };
}
const sales = table(["Quarter", "North", "South"], ["Q1", "12", "9"], ["Q2", "18", "11"], ["Q3", "15", "14"]);

test.describe("chart from table", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, title: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, title));
  }

  test("an author turns a table into a chart, which follows the table as it changes", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Sales");
    const report = await createPage(api, space.homePageId, "Report", {
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "By quarter." }] }, sales],
    });
    const path = `/s/${space.key}/p/${report.id}/report`;

    await openShowing(page, `${path}/edit`, "By quarter.");
    await editorBox(page).getByRole("cell", { name: "Q3" }).click();
    await page.locator('[data-editor-action="chart-table"]').click();
    const chart = editorBox(page).locator("[data-table-chart]").first();
    await expect(chart.locator("[data-bar]")).toHaveCount(6);
    await expect(chart.locator("figcaption")).toHaveText("North, South");

    // A row added to the table is a category more in the chart.
    await page.locator('[data-editor-action="row-below"]').click();
    // The new row is below the caret, which stays where it was.
    await editorBox(page).getByRole("row").last().getByRole("cell").first().click();
    await page.keyboard.type("Q4");
    await page.keyboard.press("Tab");
    await page.keyboard.type("30");
    await expect(chart.locator("[data-bar]")).toHaveCount(7);
    await expect(chart.locator('[data-bar="North:Q4"]')).toBeVisible();

    await editorBox(page).locator('[data-action="table-chart-kind"]').selectOption("line");
    await expect(chart.locator('polyline[data-series="North"]')).toHaveCount(1);
    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);

    const published = shown(page).locator("[data-table-chart-block]");
    const plot = published.getByRole("img", { name: "Line chart of North, South over 4 categories." });
    await expect(plot).toBeVisible();
    await expect(published.getByRole("table")).toContainText("Q4");
    // The keyboard reads the chart a category at a time.
    await plot.focus();
    await page.keyboard.press("End");
    await expect(published.locator('[data-chart-readout="Q4"]')).toContainText("30");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`charts from tables pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const target = await createPage(api, space.homePageId, "Charts", {
        type: "doc",
        content: [
          { type: "tableChart", attrs: { chart: "bar", showTable: true }, content: [sales] },
          { type: "tableChart", attrs: { chart: "pie", showTable: false }, content: [sales] },
        ],
      });
      await startInScheme(page, scheme);
      await openShowing(page, `/s/${space.key}/p/${target.id}/charts`, "North, South");
      await expect(shown(page).locator('[data-table-chart="pie"] figcaption')).toHaveText("North");
      await expect(shown(page).locator('[data-table-chart][data-state="chart"]')).toHaveCount(2);
      await page.screenshot({ path: testInfo.outputPath(`table-charts-${scheme}.png`), fullPage: true });
      await expectAccessible(page);
    });
  }
});
