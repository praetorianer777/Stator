import { expect } from "../fixtures/auth";
import { must } from "../fixtures/api";
import { orgTest as test } from "../fixtures/org";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";

test.describe("copying space permissions", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an administrator previews another space's permissions and copies them", async ({ page, api, apiAs }, testInfo) => {
    const target = uniqueKey(testInfo);
    const source = `${target.slice(0, -1)}${target.endsWith("S") ? "T" : "S"}`;
    made.push(target, source);
    const sourceName = uniqueName(testInfo, "Source");
    await createSpace(api, source, sourceName);
    await createSpace(api, target, uniqueName(testInfo, "Target"));
    const bobApi = await apiAs("bob");
    const bobId = must(await bobApi.GET("/auth/me")).user.id;
    const aliceId = must(await api.GET("/auth/me")).user.id;
    must(
      await api.PUT("/spaces/{spaceKey}/permissions", {
        params: { path: { spaceKey: source } },
        body: {
          grants: [
            { subject: { type: "user", id: aliceId }, permissions: ["administer"] },
            { subject: { type: "user", id: bobId }, permissions: ["view"] },
          ],
        },
      }),
    );

    const grid = page.getByRole("table", { name: "Permissions in this space" });
    await openShowing(page, `/s/${target}/settings?tab=permissions`, grid.locator('[data-grant-type="everyone"]'));
    await page.locator('[data-action="copy-permissions"]').click();
    const dialog = page.locator("[data-permission-copy-dialog]");
    await dialog.getByLabel("Copy from").selectOption(source);
    await dialog.locator('[data-copy-mode="replace"]').click();

    const diff = dialog.getByRole("table", { name: "Changes to this space's permissions" });
    await expect(diff.locator('[data-copy-change="Everyone"]')).toHaveAttribute("data-copy-kind", "removed");
    await expect(diff.locator('[data-copy-change="Bob Builder"]')).toHaveAttribute("data-copy-kind", "added");
    await expect(diff.locator("[data-copy-change]")).toHaveCount(2);
    await expectAccessible(page);

    await dialog.locator('[data-action="apply-permission-copy"]').click();
    await expect(page.getByText(`Copied the permissions of ${sourceName}.`, { exact: true })).toBeVisible();
    await expect(dialog).toHaveCount(0);
    await expect(grid.locator('[data-grant="Bob Builder"] [data-permission-cell="view"]')).toBeChecked();
    await expect(grid.locator('[data-grant-type="everyone"]')).toHaveCount(0);

    // Bob could add pages as one of everyone, and may now only read.
    await expect.poll(async () => must(await bobApi.GET("/spaces/{spaceKey}", { params: { path: { spaceKey: target } } })).space.can.editPages).toBe(false);

    await page.locator('[data-action="copy-permissions"]').click();
    await dialog.getByLabel("Copy from").selectOption(source);
    await dialog.locator('[data-copy-mode="replace"]').click();
    await expect(dialog.getByText("This space already grants all of that, so copying changes nothing.")).toBeVisible();
    await expect(dialog.locator('[data-action="apply-permission-copy"]')).toBeDisabled();
  });
});
