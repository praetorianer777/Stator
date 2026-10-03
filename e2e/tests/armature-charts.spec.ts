import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, publishFromEditor, uniqueKey, type Space } from "../fixtures/spaces";
import { armatureURL } from "../fixtures/stack";

// The stub makes a person of any name on first use, in the Armature
// organization its token names; alice sees CP there.
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;

const editorBox = (page: Page) => page.locator("#page-body");
const shown = (page: Page) => page.locator("main [data-doc]");
const paragraph = (value: string) => ({ type: "paragraph", content: [{ type: "text", text: value }] });

/** Opens a page until it holds the words given, which a replica may lag behind on. */
async function open(page: Page, path: string, words: string): Promise<void> {
  await expect(async () => {
    await page.goto(path);
    await expect(page.locator("main")).toContainText(words, { timeout: 2_000 });
  }).toPass();
}

test.describe("Armature charts", { tag: ["@auth"] }, () => {
  const made: string[] = [];

  test.beforeEach(async ({ api, freshOrg }) => {
    await api.DELETE("/armature/connection");
    must(await api.PUT("/armature/connection", { body: { baseUrl: armatureURL(), orgSlug: freshOrg.slug } }));
    must(await api.PUT("/armature/account/token", { body: { token: patFor(freshOrg.slug, "alice") } }));
  });

  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
    await api.DELETE("/armature/connection");
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("a lead charts a project's issues, and each reader sees the chart their own token counts", async ({ page, api, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Charts");
    const report = await createPage(api, space.homePageId, "Status report", { type: "doc", content: [paragraph("Where we are."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${report.id}/status-report`;

    await open(page, `${path}/edit`, "Where we are.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/pie");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Insert an Armature chart" });
    await expect(dialog.getByLabel("Project")).toHaveValue("CP");
    await dialog.getByLabel("Query").fill("project = CP");
    await dialog.getByLabel("Share out by").selectOption("statusCategory");
    await dialog.getByRole("button", { name: "Insert" }).click();
    const legend = editorBox(page).getByRole("table", { name: "Issues by status category" });
    await expect(legend.getByRole("row", { name: /To do/ })).toContainText("3");

    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);
    const chart = shown(page).locator('[data-armature-chart="pie"]');
    await expect(chart.getByRole("table", { name: "Issues by status category" }).getByRole("row", { name: /Done/ })).toContainText("20%");
    // The legend's row points out its slice, as the slice itself does.
    await chart.locator('[data-legend-row="To do"]').hover();
    await expect(chart.locator(".doc-chart-hole-label")).toHaveText("60%");

    // Bob has no Armature token, so his reading of the same page asks him to connect.
    const bob = await pageAs("bob");
    await open(bob, path, "Where we are.");
    await expect(bob.locator('main [data-armature-chart="pie"]')).toHaveAttribute("data-state", "connect");
    await expect(bob.locator("main")).toContainText("Connect your Armature account to see this chart.");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`charts pass axe in ${scheme}, a pie and created against resolved`, async ({ page, api, request, freshOrg }, testInfo) => {
      // An issue done today, so created against resolved has a day to draw.
      const done = await request.fetch(`${armatureURL()}/_stub/${freshOrg.slug}/issues/CP-3`, { method: "PATCH", data: { statusCategory: "done" } });
      expect(done.status()).toBe(200);
      const space = await freshSpace(api, testInfo, "Axe");
      const report = await createPage(api, space.homePageId, "Charts", {
        type: "doc",
        content: [
          { type: "armatureChart", attrs: { project: "CP", query: "project = CP", chart: "pie", groupBy: "priority", days: 30 } },
          { type: "armatureChart", attrs: { project: "CP", query: "project = CP", chart: "createdResolved", groupBy: "statusCategory", days: 30 } },
        ],
      });
      await startInScheme(page, scheme);
      await open(page, `/s/${space.key}/p/${report.id}/charts`, "Charts");
      await expect(shown(page).locator('[data-armature-chart="pie"]')).toHaveAttribute("data-state", "chart");
      const flow = shown(page).locator('[data-armature-chart="createdResolved"]');
      await expect(flow).toHaveAttribute("data-state", "chart");
      await flow.locator("[data-chart-flow]").focus();
      await page.keyboard.press("End");
      await expect(flow.locator("[data-chart-readout]")).toContainText("1 resolved");
      await expectAccessible(page);
    });
  }
});
