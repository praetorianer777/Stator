import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { ONE_LOOK, openUntil } from "../fixtures/replica";
import { expectAccessible } from "../fixtures/shell";
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

    await page.goto("/settings/example-space");
    await expect(page.locator("[data-example-exists]")).toContainText("Getting to know Stator");
    await page.locator('[data-action="create-example-space"]').click();
    await expect(page.locator("[data-example-exists]")).toContainText("The example space exists already");
    await page.locator("[data-example-exists]").getByRole("link", { name: "Getting to know Stator" }).click();
    await expect(page).toHaveURL(new RegExp(`/s/${KEY}$`));
  });
});
