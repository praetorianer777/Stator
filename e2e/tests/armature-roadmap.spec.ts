import type { APIRequestContext, Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { caretTo } from "../fixtures/editor";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openShowing, openUntil } from "../fixtures/replica";
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

/**
 * Plans CP in the stub: an epic over two issues, a third issue of another
 * team outside it. CP-5 is SEC-2, moved, so the epic is CP-6.
 */
async function plan(request: APIRequestContext, tenant: string): Promise<void> {
  const stub = (method: string, path: string, data: object) => request.fetch(`${armatureURL()}/_stub/${tenant}${path}`, { method, data });
  expect((await stub("POST", "/projects/CP/issues", { summary: "Launch the beta", type: "Epic" })).status()).toBe(201);
  for (const [key, change] of [
    ["CP-1", { parent: "CP-6", startDate: "2026-09-01", dueDate: "2026-09-20", team: "Platform" }],
    ["CP-4", { parent: "CP-6", startDate: "2026-10-01", team: "Platform" }],
    ["CP-2", { startDate: "2026-08-15", dueDate: "2026-08-31", team: "Support" }],
  ] as const) {
    expect((await stub("PATCH", `/issues/${key}`, change)).status()).toBe(200);
  }
}

test.describe("Armature roadmaps", { tag: ["@auth"] }, () => {
  const made: string[] = [];

  test.beforeEach(async ({ api, request, freshOrg }) => {
    await plan(request, freshOrg.slug);
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

  test("a product owner puts the plan on a page, by epic and then by team, and each reader sees it with their own token", async ({
    page,
    api,
    pageAs,
  }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Roadmaps");
    const notes = await createPage(api, space.homePageId, "Plan", { type: "doc", content: [paragraph("Where we are going."), { type: "paragraph" }] });
    const path = `/s/${space.key}/p/${notes.id}/plan`;

    await openShowing(page, `${path}/edit`, "Where we are going.");
    await caretTo(editorBox(page), "end");
    await page.keyboard.type("/roadmap");
    await expect(page.getByRole("listbox", { name: "Insert a block" })).toBeVisible();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Insert an Armature roadmap" });
    await expect(dialog.getByLabel("Query")).toHaveValue("project = CP AND statusCategory != done");
    await dialog.getByLabel("Query").fill("project = CP");
    await dialog.getByRole("button", { name: "Insert" }).click();
    const epic = editorBox(page).getByRole("region", { name: "Launch the beta" });
    await expect(epic.locator("[data-roadmap-row]")).toHaveText([/CP-1/, /CP-4/]);

    // The author changes the grouping from the block's own settings.
    await editorBox(page).getByRole("button", { name: "Edit roadmap" }).click();
    const edit = page.getByRole("dialog", { name: "Edit the Armature roadmap" });
    await edit.getByLabel(/^Team/).check();
    await edit.getByRole("button", { name: "Save" }).click();
    await expect(editorBox(page).getByRole("region", { name: "Platform" }).locator("[data-roadmap-row]")).toHaveText([/CP-1/, /CP-4/]);

    await expect(page.locator("[data-draft-status]")).toHaveAttribute("data-draft-status", "saved");
    await publishFromEditor(page);
    const roadmap = shown(page).locator('[data-armature-roadmap="team"]');
    await expect(roadmap).toHaveAttribute("data-state", "roadmap");
    await expect(roadmap.getByRole("region", { name: "Support" })).toContainText("Sign-in fails with an expired session");
    await expect(roadmap.getByRole("link", { name: "CP-1" })).toHaveAttribute("href", /\/issues\/CP-1$/);
    await expect(roadmap).toContainText("2 more issues have no start or due date, so they are not drawn.");
    // CP-4 was due on the 15th already; its start makes it a bar.
    await expect(roadmap.locator('[data-roadmap-row="CP-4"] .doc-roadmap-bar')).toHaveAttribute("title", "Oct 1, 2026 to Oct 15, 2026, To do");

    // Bob has no Armature token, so his reading of the same page asks him to connect.
    const bob = await pageAs("bob");
    // The first version has the same words, so wait for the block only the publish has.
    await openUntil(bob, path, () => expect(bob.locator('main [data-armature-roadmap="team"]')).toHaveAttribute("data-state", "connect", ONE_LOOK));
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`a roadmap passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const notes = await createPage(api, space.homePageId, "Roadmap", {
        type: "doc",
        content: [{ type: "armatureRoadmap", attrs: { project: "CP", query: "project = CP", groupBy: "epic" } }],
      });
      await startInScheme(page, scheme);
      await openShowing(page, `/s/${space.key}/p/${notes.id}/roadmap`, "Roadmap");
      const roadmap = shown(page).locator('[data-armature-roadmap="epic"]');
      await expect(roadmap).toHaveAttribute("data-state", "roadmap");
      await expect(roadmap.locator('[data-roadmap-group="CP-6"] .doc-roadmap-head .doc-roadmap-bar')).toHaveAttribute("data-derived", "true");
      await page.screenshot({ path: testInfo.outputPath(`roadmap-${scheme}.png`), fullPage: true });
      await expectAccessible(page);
    });
  }
});
