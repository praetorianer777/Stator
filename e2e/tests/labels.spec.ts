import type { Page, TestInfo } from "@playwright/test";
import { must, type StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createPage, createSpace, deleteSpace, uniqueKey, type Space } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const labelBox = (page: Page) => page.locator("[data-page-labels]").getByRole("combobox", { name: "Add a label" });
const chip = (page: Page, name: string) => page.locator(`[data-page-labels] [data-label="${name}"]`);

async function addLabel(api: StatorApi, pageId: string, name: string): Promise<void> {
  must(await api.POST("/pages/{pageID}/labels", { params: { path: { pageID: pageId } }, body: { name } }));
}

async function openWithLabel(page: Page, path: string, name: string): Promise<void> {
  await openShowing(page, path, chip(page, name));
}

test.describe("labels", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  async function freshSpace(api: StatorApi, testInfo: TestInfo, label: string): Promise<Space> {
    const key = uniqueKey(testInfo);
    made.push(key);
    return createSpace(api, key, uniqueName(testInfo, label));
  }

  test("adds a label picked from the suggestions and opens the label's pages", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Label");
    const tagged = await createPage(api, space.homePageId, "Rollout checklist");
    const target = await createPage(api, space.homePageId, "Rollout retro");
    const word = `rollout-${testInfo.workerIndex}${Date.now().toString(36)}`;
    await addLabel(api, tagged.id, word);

    await page.goto(`/s/${space.key}/p/${target.id}/rollout-retro`);
    await expect(heading(page)).toHaveText("Rollout retro");
    const box = labelBox(page);
    await expect(async () => {
      await box.fill("");
      await box.fill(word.slice(0, 10));
      await expect(page.locator(`[data-label-option="${word}"]`)).toBeVisible(ONE_LOOK);
    }).toPass();
    // The first option is what was typed; the one other pages carry is next.
    await expect(page.locator("[data-label-option]").first()).toHaveAttribute("aria-selected", "true");
    await box.press("ArrowDown");
    await expect(box).toHaveAttribute("aria-activedescendant", (await page.locator(`[data-label-option="${word}"]`).getAttribute("id")) ?? "");
    await box.press("Enter");
    await expect(chip(page, word)).toBeVisible();
    await expect(box).toHaveValue("");
    await expect(box).toBeFocused();

    await chip(page, word).getByRole("link", { name: word }).click();
    await expect(page).toHaveURL(new RegExp(`/s/${space.key}/labels/${word}$`));
    await expect(page.locator("[data-label-heading]")).toHaveText(word);
    await openShowing(page, `/s/${space.key}/labels/${word}`, page.locator('[data-labeled-page="Rollout retro"]'));
    await expect(page.locator('[data-labeled-page="Rollout checklist"]')).toBeVisible();

    await page.locator('[data-action="label-everywhere"]').click();
    await expect(page).toHaveURL(new RegExp(`/labels/${word}$`));
    await expect(page.locator("[data-labeled-page]")).toHaveCount(2);
    await page.locator('[data-labeled-page="Rollout checklist"]').getByRole("link", { name: "Rollout checklist" }).click();
    await expect(heading(page)).toHaveText("Rollout checklist");
  });

  test("a new label is typed, and taken off again", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Typed");
    const target = await createPage(api, space.homePageId, "Budget");

    await page.goto(`/s/${space.key}/p/${target.id}/budget`);
    const box = labelBox(page);
    await box.fill("Finance Q4");
    await box.press("Enter");
    await expect(chip(page, "finance-q4")).toBeVisible();
    await box.fill("no/slash");
    await expect(page.locator("[data-page-labels] [data-label-status]")).toContainText("Use letters, digits");
    await expect(box).toHaveAttribute("aria-invalid", "true");
    await box.fill("");

    await chip(page, "finance-q4").getByRole("button", { name: "Remove label finance-q4" }).click();
    await expect(chip(page, "finance-q4")).toHaveCount(0);
    await expect.poll(async () => must(await api.GET("/pages/{pageID}/labels", { params: { path: { pageID: target.id } } })).labels).toEqual([]);
  });

  test("search narrows its hits to a label picked in the filter", async ({ page, api }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Filter");
    const word = `gannet-${testInfo.workerIndex}${Date.now().toString(36)}`;
    const labeled = await createPage(api, space.homePageId, "Gannet census labeled");
    await createPage(api, space.homePageId, "Gannet census plain");
    await addLabel(api, labeled.id, word);

    await openShowing(page, "/search?q=gannet", page.locator('[data-search-hit="Gannet census plain"]'));
    const box = page.locator('[data-filter="label"]').getByRole("combobox", { name: "Labels" });
    await expect(async () => {
      await box.fill("");
      await box.fill(word);
      await expect(page.locator(`[data-label-option="${word}"]`)).toBeVisible(ONE_LOOK);
    }).toPass();
    await box.press("Enter");
    await expect(page).toHaveURL(new RegExp(`label=${word}`));
    await expect(page.locator('[data-search-hit="Gannet census labeled"]')).toBeVisible();
    await expect(page.locator('[data-search-hit="Gannet census plain"]')).toHaveCount(0);
    await expect(page.locator('[data-search-hit="Gannet census labeled"]').locator(`[data-label="${word}"]`)).toBeVisible();

    await page.locator(`[data-label-filters] [data-label="${word}"]`).getByRole("button").click();
    await expect(page).not.toHaveURL(/label=/);
    await expect(page.locator('[data-search-hit="Gannet census plain"]')).toBeVisible();
  });

  test("a member who may not edit reads the labels and is offered no way to change them", async ({ api, apiAs, pageAs }, testInfo) => {
    const space = await freshSpace(api, testInfo, "Read only");
    const target = await createPage(api, space.homePageId, "Charter");
    await addLabel(api, target.id, "governance");
    must(
      await api.PUT("/spaces/{spaceKey}/permissions", {
        params: { path: { spaceKey: space.key } },
        body: { grants: [{ subject: { type: "everyone" }, permissions: ["view"] }] },
      }),
    );
    const bobApi = await apiAs("bob");
    await expect.poll(async () => (await bobApi.GET("/pages/{pageID}", { params: { path: { pageID: target.id } } })).data?.page.can.edit).toBe(false);
    expect((await bobApi.POST("/pages/{pageID}/labels", { params: { path: { pageID: target.id } }, body: { name: "mine" } })).response.status).toBe(403);

    const bob = await pageAs("bob");
    await openWithLabel(bob, `/s/${space.key}/p/${target.id}/charter`, "governance");
    await expect(bob.locator("[data-page-labels]").getByRole("combobox")).toHaveCount(0);
    await expect(chip(bob, "governance").getByRole("button")).toHaveCount(0);
    await expect(bob.locator('[data-action="edit-page"]')).toHaveCount(0);
    await chip(bob, "governance").getByRole("link", { name: "governance" }).click();
    await expect(bob.locator('[data-labeled-page="Charter"]')).toBeVisible();
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`labels on a page and the label page pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const space = await freshSpace(api, testInfo, "Axe");
      const target = await createPage(api, space.homePageId, "Puffin colony");
      await addLabel(api, target.id, "seabirds");
      await startInScheme(page, scheme);

      await openWithLabel(page, `/s/${space.key}/p/${target.id}/puffin-colony`, "seabirds");
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expectAccessible(page);

      const box = labelBox(page);
      await box.fill("sea");
      await expect(page.locator("[data-page-labels] [data-label-option]").first()).toBeVisible();
      await box.press("ArrowDown");
      await expectAccessible(page);
      await box.fill("");

      await chip(page, "seabirds").getByRole("link", { name: "seabirds" }).click();
      await expect(page.locator('[data-labeled-page="Puffin colony"]')).toBeVisible();
      await expectAccessible(page);
    });
  }
});
