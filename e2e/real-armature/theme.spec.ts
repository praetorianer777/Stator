import type { Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { teamTest as test } from "../fixtures/team";
import { connectAccount, connectSite, readSeed } from "./armature";

// Following the theme Mia uses in the real Armature, which the seed made
// from one of Armature's example themes and chose for her there.

const rootVar = (page: Page, name: string) => page.evaluate((n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim(), name);
const followSwitch = (page: Page) => page.getByRole("switch", { name: "Follow my Armature theme" });

test.describe("following a real Armature's theme", { tag: ["@auth"] }, () => {
  test.beforeEach(async ({ api }) => {
    const seed = readSeed();
    await connectSite(api, seed);
    await connectAccount(api, seed.people.member);
  });

  test.afterEach(async ({ api }) => {
    await api.DELETE("/armature/theme");
    await api.DELETE("/armature/connection");
  });

  test("alice follows the theme she uses in Armature", async ({ page, api }) => {
    const seed = readSeed();
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/settings/themes");
    const accentBefore = await rootVar(page, "--color-accent");
    await expect(followSwitch(page)).toHaveAttribute("aria-checked", "false");
    await expect(page.locator("style#stator-theme")).toHaveCount(0);

    await followSwitch(page).click();
    await expect(followSwitch(page)).toHaveAttribute("aria-checked", "true");
    await expect(page.locator("main")).toContainText(`You are using ${seed.theme.name}, your theme in Armature.`);
    await expect(page.locator("style#stator-theme")).toBeAttached();
    await expect.poll(() => rootVar(page, "--color-accent")).not.toBe(accentBefore);

    const { follow } = must(await api.GET("/armature/theme"));
    expect(follow).toMatchObject({ following: true, status: "ok", error: null });

    await followSwitch(page).click();
    await expect(page.locator("style#stator-theme")).toHaveCount(0);
  });
});
