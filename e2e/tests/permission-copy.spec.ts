import { expect } from "../fixtures/auth";
import { must } from "../fixtures/api";
import { teamTest as test } from "../fixtures/team";
import { openShowing } from "../fixtures/replica";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible } from "../fixtures/shell";
import { createSpace, deleteSpace, uniqueKey } from "../fixtures/spaces";
import { setSpaceGrants } from "../fixtures/grants";

test.describe("copying space permissions", { tag: ["@auth"] }, () => {
  const made: string[] = [];
  test.afterEach(async ({ api }) => {
    for (const key of made.splice(0)) await deleteSpace(api, key);
  });

  test("an administrator previews another space's permissions and copies them", async ({ page, api, apiAs, team }, testInfo) => {
    const target = uniqueKey(testInfo);
    const source = `${target.slice(0, -1)}${target.endsWith("S") ? "T" : "S"}`;
    made.push(target, source);
    const sourceName = uniqueName(testInfo, "Source");
    await createSpace(api, source, sourceName);
    await createSpace(api, target, uniqueName(testInfo, "Target"));
    const bobApi = await apiAs("bob");
    const bobId = must(await bobApi.GET("/auth/me")).user.id;
    const aliceId = must(await api.GET("/auth/me")).user.id;
    await setSpaceGrants(api, source, [
      { subject: { type: "user", id: aliceId }, permissions: ["administer"] },
      { subject: { type: "user", id: bobId }, permissions: ["view"] },
    ]);

    const grid = page.getByRole("table", { name: "Roles granted in this space" });
    await openShowing(page, `/spaces/${target}/settings/members`, grid.locator('[data-grant="Everyone"]'));
    await page.locator('[data-action="copy-permissions"]').click();
    const dialog = page.locator("[data-permission-copy-dialog]");
    await dialog.getByLabel("Copy from").selectOption(source);
    await dialog.locator('[data-copy-mode="replace"]').click();

    const diff = dialog.getByRole("table", { name: "Changes to this space's permissions" });
    await expect(diff.locator('[data-copy-change="Everyone"]')).toHaveAttribute("data-copy-kind", "removed");
    await expect(diff.locator(`[data-copy-change="${team.people.bob.name}"]`)).toHaveAttribute("data-copy-kind", "added");
    await expect(diff.locator("[data-copy-change]")).toHaveCount(2);
    await expectAccessible(page);

    await dialog.locator('[data-action="apply-permission-copy"]').click();
    await expect(page.getByText(`Copied the permissions of ${sourceName}.`, { exact: true })).toBeVisible();
    await expect(dialog).toHaveCount(0);
    await expect(grid.locator(`[data-grant="${team.people.bob.name}"][data-grant-role="viewer"]`)).toBeVisible();
    await expect(grid.locator('[data-grant="Everyone"]')).toHaveCount(0);

    // Bob could add pages through Everyone, and may now only read.
    await expect.poll(async () => must(await bobApi.GET("/spaces/{spaceKey}", { params: { path: { spaceKey: target } } })).space.can.editPages).toBe(false);

    await page.locator('[data-action="copy-permissions"]').click();
    await dialog.getByLabel("Copy from").selectOption(source);
    await dialog.locator('[data-copy-mode="replace"]').click();
    await expect(dialog.getByText("This space already grants all of that, so copying changes nothing.")).toBeVisible();
    await expect(dialog.locator('[data-action="apply-permission-copy"]')).toBeDisabled();
  });
});
