import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { Page } from "@playwright/test";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { chooseTheme, createTheme, listThemes, themeSpec, uniqueName, updateTheme, uploadThemeAsset } from "../fixtures/seed";

const MINECRAFT = resolve(dirname(fileURLToPath(import.meta.url)), "../../backend/internal/theme/testdata/minecraft.armature-theme.json");
const MINECRAFT_NAME = JSON.parse(readFileSync(MINECRAFT, "utf8")).name as string;

const SAFE_SVG = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 2h12v12H2z"/></svg>';
const UNSAFE_SVG = '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>';

// Long enough for a moving canvas to change between two looks at it.
const MOTION_SAMPLE_MS = 400;

const THEMES_PATH = "/settings/themes";
const customStyle = (page: Page) => page.locator("style#stator-theme");
const row = (page: Page, name: string) => page.locator(`[data-theme-row="${name}"]`);
const rootVar = (page: Page, name: string) => page.evaluate((n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim(), name);

async function rowAction(page: Page, name: string, action: string) {
  await row(page, name).locator('[data-action="theme-menu"]').click();
  await page.locator(`[role="menu"] [data-action="${action}"]`).click();
}

// A person reads their own writes at once, the session pinning their reads to
// them. Another person's write reaches a reader only once the replica has it,
// so where one person looks at what another wrote, the page reloads until it
// has arrived.
const REPLICA_CATCH_UP_MS = 10_000;
const RECHECK_MS = 1_000;
async function afterReplication(page: Page, check: () => Promise<void>) {
  await expect(async () => {
    await page.reload();
    await check();
  }).toPass({ timeout: REPLICA_CATCH_UP_MS, intervals: [RECHECK_MS] });
}

const styleText = (page: Page) => customStyle(page).evaluate((el) => el.textContent ?? "");

async function pickScheme(page: Page, choice: "system" | "light" | "dark") {
  const button = page.locator('[data-action="theme"]').first();
  for (let i = 0; i < 3 && (await button.getAttribute("data-theme-choice")) !== choice; i++) await button.click();
  await expect(button).toHaveAttribute("data-theme-choice", choice);
}

// The organisation is this file's alone, so its default and in-use counts are
// nobody else's; tests that share a worker share it one after another, and
// each starts with nobody's choice and no default.
test.describe("themes", { tag: ["@auth", "@desktop"] }, () => {
  test.afterEach(async ({ api, apiAs }) => {
    for (const each of [api, await apiAs("bob")]) await chooseTheme(each, null).catch(() => {});
    await api.PUT("/themes/default", { body: { themeId: null } });
  });

  test("the chosen theme applies in the shell and survives a reload without a flash", async ({ page, api }, testInfo) => {
    const name = uniqueName(testInfo, "canvas");
    await createTheme(api, name, themeSpec({ colors: { light: { canvas: "#123456" }, dark: { canvas: "#123456" } } }));
    await page.goto(THEMES_PATH);
    await rowAction(page, name, "use-theme");
    await expect(page.locator("[data-themes-notice]")).toHaveText(`Now using ${name}.`);

    await page.goto("/");
    await expect(customStyle(page)).toBeAttached();
    expect(await rootVar(page, "--color-canvas")).toBe("#123456");

    // Recorded the moment the app first draws anything, before the server has answered.
    await page.addInitScript(() => {
      new MutationObserver((_, observer) => {
        if (document.getElementById("root")?.firstChild) {
          (window as unknown as { themeAtFirstDraw: boolean }).themeAtFirstDraw = Boolean(document.getElementById("stator-theme"));
          observer.disconnect();
        }
      }).observe(document, { childList: true, subtree: true });
    });
    await page.reload();
    // The load event can fire before the first draw: the app draws nothing
    // until the server has said who is signed in.
    await page.waitForFunction(() => "themeAtFirstDraw" in window);
    expect(await page.evaluate(() => (window as unknown as { themeAtFirstDraw?: boolean }).themeAtFirstDraw)).toBe(true);
    expect(await rootVar(page, "--color-canvas")).toBe("#123456");
  });

  test("light, dark and auto each apply their palette while a custom theme is on", async ({ page, api }, testInfo) => {
    const theme = await createTheme(
      api,
      uniqueName(testInfo, "palettes"),
      themeSpec({ colors: { light: { canvas: "#fafaf0" }, dark: { canvas: "#10202a" } } }),
    );
    await chooseTheme(api, theme.id);
    await page.goto("/");
    await expect(customStyle(page)).toBeAttached();

    await pickScheme(page, "light");
    expect(await rootVar(page, "--color-canvas")).toBe("#fafaf0");
    await pickScheme(page, "dark");
    expect(await rootVar(page, "--color-canvas")).toBe("#10202a");
    await pickScheme(page, "system");
    await page.emulateMedia({ colorScheme: "dark" });
    expect(await rootVar(page, "--color-canvas")).toBe("#10202a");
    await page.emulateMedia({ colorScheme: "light" });
    expect(await rootVar(page, "--color-canvas")).toBe("#fafaf0");
  });

  test("the imported Minecraft theme gives square corners and hard shadows", async ({ page }) => {
    await page.goto(THEMES_PATH);
    await page.locator("[data-theme-file]").setInputFiles(MINECRAFT);
    await expect(page.locator("[data-themes-notice]")).toContainText(`Imported ${MINECRAFT_NAME}`);
    const importedName = (await page.locator("[data-themes-notice]").textContent())!.replace(/^Imported /, "").replace(/\.$/, "");
    await rowAction(page, importedName, "use-theme");
    await expect(page.locator("[data-themes-notice]")).toHaveText(`Now using ${importedName}.`);

    await page.goto("/");
    await expect(customStyle(page)).toBeAttached();
    await expect(page.locator('[data-action="search"]')).toHaveCSS("border-radius", "0px");
    await page.locator('[data-action="account"]').click();
    const shadow = await page.getByRole("menu").evaluate((el) => getComputedStyle(el).boxShadow);
    expect(shadow).toMatch(/\b6px 6px 0px\b/);
  });

  test("a theme exported and imported again gets a (2) name and new file ids", async ({ page, api }, testInfo) => {
    const name = uniqueName(testInfo, "export");
    const original = await createTheme(api, name);
    const asset = await uploadThemeAsset(api, original.id, "square.svg", "image/svg+xml", SAFE_SVG);
    await updateTheme(api, original.id, { spec: themeSpec({ icons: { home: { assetId: asset.id } } }) });

    await page.goto(THEMES_PATH);
    const download = page.waitForEvent("download");
    await rowAction(page, name, "export-theme");
    const file = testInfo.outputPath("exported.armature-theme.json");
    await (await download).saveAs(file);

    await page.locator("[data-theme-file]").setInputFiles(file);
    await expect(page.locator("[data-themes-notice]")).toHaveText(`Imported ${name} (2).`);
    await expect(row(page, `${name} (2)`)).toBeVisible();

    const copy = (await listThemes(api)).find((each) => each.name === `${name} (2)`);
    expect(copy, "the imported copy is listed").toBeDefined();
    expect(copy!.assets).toHaveLength(1);
    expect(copy!.assets[0]!.id).not.toBe(asset.id);
    expect(copy!.spec.icons.home?.assetId).toBe(copy!.assets[0]!.id);
  });

  for (const example of ["deep-tech", "constellation"]) {
    test(`a theme made from ${example} shows the network canvas, still under reduced motion`, async ({ page }, testInfo) => {
      await page.goto(`${THEMES_PATH}/new`);
      await page.locator(`[data-theme-example="${example}"]`).click();
      await page.locator("#field-theme-name").fill(uniqueName(testInfo, example));
      await page.locator('[data-theme-tab="backdrop"]').click();
      await page.locator('[data-theme-effect="constellation"]').click();
      await page.locator('[data-action="save-theme"]').click();
      await expect(page).toHaveURL(/\/settings\/themes\/[0-9a-f-]{36}$/);
      await page.locator('[data-action="use-theme"]').click();
      await expect(page.locator("[data-theme-notice]")).toContainText("Now using");

      const canvas = page.locator('[data-backdrop-effect="constellation"] canvas');
      const frame = () => canvas.evaluate((el) => (el as HTMLCanvasElement).toDataURL());
      await page.goto("/");
      await expect(canvas).toBeAttached();
      const moving = await frame();
      await page.waitForTimeout(MOTION_SAMPLE_MS);
      expect(await frame()).not.toBe(moving);

      await page.emulateMedia({ reducedMotion: "reduce" });
      await page.reload();
      await expect(canvas).toBeAttached();
      const still = await frame();
      await page.waitForTimeout(MOTION_SAMPLE_MS);
      expect(await frame()).toBe(still);
    });
  }

  test("in the editor, live preview turns on and off, and the theme saves", async ({ page, api }, testInfo) => {
    const name = uniqueName(testInfo, "preview");
    const theme = await createTheme(api, name);
    await page.goto(`${THEMES_PATH}/${theme.id}`);
    await expect(page.locator('[data-action="save-theme"]')).toBeVisible();
    await expect(customStyle(page)).toHaveCount(0);

    await page.locator('[data-token="canvas"][data-token-mode="light"]').fill("#abcdef");
    await page.locator('[data-action="preview-theme"]').click();
    await expect.poll(() => styleText(page)).toContain("--color-canvas: #abcdef");
    await page.locator('[data-action="preview-theme"]').click();
    await expect(customStyle(page)).toHaveCount(0);

    await page.locator('[data-action="save-theme"]').click();
    await expect(page.locator("[data-theme-notice]")).toHaveText(`Saved ${name}.`);
    expect((await listThemes(api)).find((each) => each.id === theme.id)?.spec.colors.light.canvas).toBe("#abcdef");
  });

  test("an uploaded SVG replaces a glyph, an unsafe one is refused, and a file in use stays", async ({ page, api }, testInfo) => {
    const name = uniqueName(testInfo, "icons");
    const theme = await createTheme(api, name);
    await page.goto(`${THEMES_PATH}/${theme.id}`);
    await page.locator('[data-theme-tab="files"]').click();
    const upload = page.locator("[data-theme-file-input]");

    await upload.setInputFiles({ name: "evil.svg", mimeType: "image/svg+xml", buffer: Buffer.from(UNSAFE_SVG) });
    await expect(page.getByRole("alert")).toContainText("script");
    await expect(page.locator('[data-theme-asset="evil.svg"]')).toHaveCount(0);

    await upload.setInputFiles({ name: "square.svg", mimeType: "image/svg+xml", buffer: Buffer.from(SAFE_SVG) });
    await expect(page.locator("[data-theme-notice]")).toContainText("square.svg");
    await expect(page.locator('[data-theme-asset="square.svg"]')).toBeVisible();

    await page.locator('[data-theme-tab="icons"]').click();
    await page.getByLabel("home icon picture").selectOption({ label: "square.svg" });
    await page.locator('[data-action="save-theme"]').click();
    await expect(page.locator("[data-theme-notice]")).toHaveText(`Saved ${name}.`);

    await page.locator('[data-theme-tab="files"]').click();
    await expect(page.locator('[data-theme-asset="square.svg"] [data-action="remove-theme-file"]')).toBeDisabled();

    await page.locator('[data-action="use-theme"]').click();
    await expect(page.locator("[data-theme-notice]")).toHaveText(`Now using ${name}.`);
    await page.goto("/");
    const home = page.locator('svg[data-icon="home"]').first();
    await expect(home).toBeVisible();
    await expect.poll(() => home.evaluate((el) => getComputedStyle(el).maskImage)).toContain(`/themes/${theme.id}/assets/`);
    await expect(home.locator("path").first()).toHaveCSS("display", "none");
  });

  test("once shared and chosen by another member, the in-use count says so", async ({ page, api, pageAs }, testInfo) => {
    const name = uniqueName(testInfo, "shared");
    await createTheme(api, name, themeSpec(), true);
    await page.goto(THEMES_PATH);
    await expect(row(page, name)).toContainText("0 people");

    const bob = await pageAs("bob");
    await bob.goto(THEMES_PATH);
    await afterReplication(bob, async () => {
      await bob.locator('[data-themes-view="shared"]').click();
      await expect(row(bob, name)).toBeVisible({ timeout: RECHECK_MS });
    });
    await rowAction(bob, name, "use-theme");
    await expect(bob.locator("[data-themes-notice]")).toHaveText(`Now using ${name}.`);

    await afterReplication(page, () => expect(row(page, name)).toContainText("1 person", { timeout: RECHECK_MS }));
  });

  test("the organisation default reaches a member without a choice, who can go back, and unsharing removes it", async ({ page, api, pageAs }, testInfo) => {
    const name = uniqueName(testInfo, "default");
    await createTheme(api, name, themeSpec({ colors: { light: { canvas: "#0f0f0f" }, dark: { canvas: "#0f0f0f" } } }), true);

    await page.goto(THEMES_PATH);
    await rowAction(page, name, "default-theme");
    await expect(page.locator("[data-themes-notice]")).toHaveText(`${name} is what everybody sees until they choose.`);
    await expect(row(page, name)).toHaveAttribute("data-theme-default", "true");

    const bob = await pageAs("bob");
    await bob.goto(THEMES_PATH);
    await afterReplication(bob, () => expect(bob.getByText(`You are using ${name}, the organization's default.`)).toBeVisible({ timeout: RECHECK_MS }));
    await expect(customStyle(bob)).toBeAttached();

    await bob.locator('[data-action="built-in-theme"]').click();
    await expect(bob.getByText(`You are using the built-in theme, over the organization's default ${name}.`)).toBeVisible();
    await expect(customStyle(bob)).toHaveCount(0);

    await rowAction(page, name, "share-theme");
    await expect(page.locator("[data-themes-notice]")).toHaveText(`${name} is yours alone again.`);
    await expect(row(page, name)).toHaveAttribute("data-theme-default", "false");
    await afterReplication(bob, () => expect(bob.getByText("You are using the built-in theme.")).toBeVisible({ timeout: RECHECK_MS }));
  });

  test("deleting a theme in use sends its users back to the built-in theme", async ({ page, api, apiAs, pageAs }, testInfo) => {
    const name = uniqueName(testInfo, "doomed");
    const theme = await createTheme(api, name, themeSpec({ colors: { light: { canvas: "#0e0e0e" }, dark: { canvas: "#0e0e0e" } } }), true);
    await chooseTheme(await apiAs("bob"), theme.id);
    const bob = await pageAs("bob");
    await bob.goto("/");
    await expect(customStyle(bob)).toBeAttached();

    await page.goto(THEMES_PATH);
    page.once("dialog", (dialog) => dialog.accept());
    await rowAction(page, name, "delete-theme");
    await expect(page.locator("[data-themes-notice]")).toContainText(name);
    await expect(row(page, name)).toHaveCount(0);

    await bob.goto(THEMES_PATH);
    await afterReplication(bob, () => expect(bob.getByText("You are using the built-in theme.")).toBeVisible({ timeout: RECHECK_MS }));
    await expect(customStyle(bob)).toHaveCount(0);
  });
});
