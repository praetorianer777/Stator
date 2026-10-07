import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { openShowing } from "../fixtures/replica";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { deleteSpace, uniqueKey } from "../fixtures/spaces";

const heading = (page: Page) => page.locator("main").getByRole("heading", { level: 1 });
const personal = (page: Page) => page.locator("[data-personal-spaces]");

// Each person makes one personal space, and a worker's tests share its
// organization one after another, so every test removes the one it made.
test.describe("personal spaces", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("alice makes a personal space, which bob finds only once it is shared", async ({ page, api, pageAs }, testInfo) => {
    const key = uniqueKey(testInfo);
    made.push(key);

    await page.goto("/spaces");
    await page.locator('[data-action="new-personal-space"]').click();
    await expect(page).toHaveURL(/\/spaces\/new\/personal$/);
    await expect(page.getByLabel("Name", { exact: true })).toHaveValue(/'s space$/);
    await page.getByLabel("Key", { exact: true }).fill(key);
    await page.locator('[data-action="create-space"]').click();
    await expect(page).toHaveURL(new RegExp(`/s/${key}$`));
    // The heading names the space once it has loaded, not while it does.
    await expect(heading(page)).toHaveText(/'s space$/);
    const name = await heading(page).textContent();

    await page.goto("/spaces");
    await expect(personal(page).locator(`[data-space-row="${key}"]`)).toContainText("You");
    await expect(page.locator('[data-action="new-personal-space"]')).toHaveCount(0);

    const bob = await pageAs("bob");
    await bob.goto("/spaces");
    await expect(bob.locator("[data-space-directory]")).toBeVisible();
    await expect(bob.locator(`[data-space-row="${key}"]`)).toHaveCount(0);
    await bob.goto(`/s/${key}`);
    await expect(bob.getByText(/not found/i).first()).toBeVisible();

    must(
      await api.PUT("/spaces/{spaceKey}/permissions", {
        params: { path: { spaceKey: key } },
        body: { grants: [{ subject: { type: "everyone" }, permissions: ["view"] }] },
      }),
    );
    await openShowing(bob, "/spaces", personal(bob).locator(`[data-space-row="${key}"]`));
    await expect(personal(bob).locator(`[data-space-row="${key}"]`)).not.toContainText("You");
    await bob.goto(`/s/${key}`);
    await expect(heading(bob)).toHaveText(name ?? "");
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the directory with a personal space and its form pass axe in ${scheme}`, async ({ pageAs }, testInfo) => {
      const bob = await pageAs("bob");
      await startInScheme(bob, scheme);
      await bob.goto("/spaces/new/personal");
      await expect(bob.getByLabel("Name", { exact: true })).toHaveValue(/'s space$/);
      await expectAccessible(bob);

      const key = uniqueKey(testInfo);
      await bob.getByLabel("Key", { exact: true }).fill(key);
      await bob.locator('[data-action="create-space"]').click();
      await expect(bob).toHaveURL(new RegExp(`/s/${key}$`));
      made.push(key);
      await bob.goto("/spaces");
      await expect(personal(bob).locator(`[data-space-row="${key}"]`)).toBeVisible();
      await expectAccessible(bob);
    });
  }
});
