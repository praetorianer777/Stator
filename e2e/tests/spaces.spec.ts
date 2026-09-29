import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });

test.describe("spaces", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an administrator creates a space and lands on its home page", async ({ page }, testInfo) => {
    const key = uniqueKey(testInfo);
    const name = uniqueName(testInfo, "Handbook");
    made.push(key);

    await page.goto("/spaces");
    await page.locator('[data-action="new-space"]').first().click();
    await expect(page).toHaveURL(/\/spaces\/new$/);
    await page.getByLabel("Name", { exact: true }).fill(name);
    await expect(page.getByLabel("Key", { exact: true })).not.toHaveValue("");
    await page.getByLabel("Key", { exact: true }).fill(key.toLowerCase());
    await page.getByLabel("Description", { exact: true }).fill("How we work.");
    await page.locator('[data-action="create-space"]').click();

    await expect(page).toHaveURL(new RegExp(`/s/${key}$`));
    await expect(heading(page)).toHaveText(name);
    await expect(page.locator("[data-page-home]")).toBeAttached();

    await page.goto("/spaces");
    await expect(page.locator(`[data-space-row="${key}"]`)).toContainText("How we work.");
  });

  test("the home page is edited and read back", async ({ page, api }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    await createSpace(api, key, uniqueName(testInfo, "Edited"));

    await page.goto(`/s/${key}`);
    await page.locator('[data-action="edit-page"]').click();
    await expect(page).toHaveURL(new RegExp(`/s/${key}/p/[0-9a-f-]+/[^/]+/edit$`));
    await page.getByLabel("Title", { exact: true }).fill("Welcome");
    await page.locator("#page-body").click();
    await page.keyboard.type("Everything starts here.");
    await page.locator('[data-action="save-page"]').click();

    await expect(page).toHaveURL(new RegExp(`/s/${key}$`));
    await expect(heading(page)).toHaveText("Welcome");
    await expect(page.locator("[data-doc]")).toContainText("Everything starts here.");
  });

  test("a member sees the space but cannot make or change one", async ({ api, pageAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);
    await createSpace(api, key, uniqueName(testInfo, "Shared"));
    const bob = await pageAs("bob");

    // Bob reads what alice wrote, which a replica may not have yet.
    await expect(async () => {
      await bob.goto("/spaces");
      await expect(bob.locator(`[data-space-row="${key}"]`)).toBeVisible({ timeout: 1_000 });
    }).toPass();
    await expect(bob.locator('[data-action="new-space"]')).toHaveCount(0);
    await bob.goto(`/s/${key}/settings`);
    await expect(bob.getByText("Only an administrator of your organization can change this space's details.")).toBeVisible();
    await expect(bob.locator('[data-action="delete-space"]')).toHaveCount(0);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the space home passes axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const key = uniqueKey(testInfo);
      made.push(key);
      await createSpace(api, key, uniqueName(testInfo, "Accessible"));
      await startInScheme(page, scheme);
      await page.goto(`/s/${key}`);
      await expect(page.locator("[data-page-home]")).toBeAttached();
      await expectAccessible(page);
    });
  }
});
