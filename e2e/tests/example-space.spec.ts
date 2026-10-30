import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { ageExample } from "../fixtures/testapi";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible, openAdminPage } from "../fixtures/shell";
import { deleteSpace } from "../fixtures/spaces";

const KEY = "STATOR";
const SHOWCASE = "Every block a page can hold";
// The worker makes the space, publishing some twenty pages, files and posts
// through the services one by one, which takes seconds on a busy machine.
const MAKING = { timeout: 30_000 } as const;

// Blocks the showcase draws without asking anybody but Stator, by the mark each view sets.
const DRAWN = [
  "data-panel",
  "data-columns",
  "data-math",
  "data-diagram",
  "data-decision",
  "data-status",
  "data-date",
  "data-excerpt",
  "data-include",
  "data-properties",
  "data-properties-report",
  "data-table-chart-block",
  "data-task-report",
  "data-team-calendar",
  "data-contributors",
  "data-link-card",
  "data-attachment-list",
];

test.describe("the example space", { tag: ["@auth"] }, () => {
  test.afterEach(async ({ api }) => {
    const { space } = must(await api.GET("/example-space"));
    if (space) await deleteSpace(api, space.key);
  });

  test("an administrator makes it from the spaces overview, reads the showcase, and finds it there the next time", async ({ page, pageAs }) => {
    test.slow();
    await page.goto("/spaces");
    await page.locator('[data-action="create-example-space"]').click();
    await expect(page).toHaveURL(new RegExp(`/s/${KEY}$`), MAKING);
    await expect(page.getByRole("heading", { level: 1, name: "Getting to know Stator" })).toBeVisible();

    const doc = page.locator("[data-doc]").first();
    await doc.getByRole("link", { name: SHOWCASE }).first().click();
    await expect(page.getByRole("heading", { level: 1, name: SHOWCASE })).toBeVisible();
    const cover = page.locator("[data-page-cover] img");
    await expect(cover).toBeVisible();
    await expect.poll(() => cover.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);
    await expect(doc).toContainText("The picture across the top of this page is its cover.");
    for (const mark of DRAWN) await expect(doc.locator(`[${mark}]`).first(), mark).toBeVisible();
    await expect(doc.getByRole("img", { name: "A page of text beside a bar chart" })).toBeVisible();
    await expect(doc.locator("[data-include]")).toContainText("Stator keeps a team's knowledge as pages");
    await expect(doc).toContainText("This organization has no Armature connected");
    await expectAccessible(page);

    // Everybody reads it, and nobody but an administrator is offered to make it.
    const bob = await pageAs("bob");
    await openUntil(bob, `/s/${KEY}`, () => expect(bob.locator("[data-doc]").getByRole("link", { name: SHOWCASE }).first()).toBeVisible(ONE_LOOK));
    await bob.goto("/spaces");
    await expect(bob.getByRole("link", { name: "Getting to know Stator" })).toBeVisible();
    await expect(bob.locator('[data-action="create-example-space"]')).toHaveCount(0);

    await openAdminPage(page, "example-space-settings");
    await expect(page.locator("[data-example-exists]")).toContainText("Getting to know Stator");
    await page.locator('[data-action="create-example-space"]').click();
    await expect(page.locator("[data-example-exists]")).toContainText("The example space exists already");
    await page.locator("[data-example-exists]").getByRole("link", { name: "Getting to know Stator" }).click();
    await expect(page).toHaveURL(new RegExp(`/s/${KEY}$`));
  });

  test("an administrator sees that a newer one is available, renews it, and reads the new guide", async ({ page, api }) => {
    test.slow();
    must(await api.POST("/example-space", { body: { language: "en" } }));
    await expect.poll(async () => must(await api.GET("/example-space")).space?.key ?? null, MAKING).toBe(KEY);
    await ageExample(KEY, "moving-pages");

    await page.goto("/spaces");
    const renewal = page.locator("[data-example-renewal]");
    await expect(renewal).toContainText("A newer example space is available");
    await expect(renewal.locator("[data-example-new] li").first()).toContainText("Moving pages in and out");
    await expectAccessible(page);
    await renewal.locator('[data-action="renew-example-space"]').click();
    await expect(page.locator("[data-example-renewed]")).toBeVisible(MAKING);
    await page.locator("[data-renewal-added]").getByRole("link", { name: "Moving pages in and out" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Moving pages in and out" })).toBeVisible();
    await expect(page.locator("[data-doc]").first()).toContainText("Import space");

    await page.goto(`/s/${KEY}`);
    await expect(page.getByRole("heading", { level: 1, name: "Getting to know Stator" })).toBeVisible();
    await expect(page.locator("[data-example-renewal]")).toHaveCount(0);
  });
});
