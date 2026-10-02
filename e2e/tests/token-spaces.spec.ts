import type { Page, TestInfo } from "@playwright/test";
import type { StatorApi } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { uniqueName } from "../fixtures/seed";
import { expectAccessible, scrollsSideways, startInScheme, type ColourScheme } from "../fixtures/shell";
import { createSpace, uniqueKey } from "../fixtures/spaces";
import { WEB_URL } from "../fixtures/stack";

const TOKENS_PATH = "/settings/tokens";
// A space the alice who made them just wrote may still be on its way to the
// replica the token reads, so the first read is tried until it lands.
const REPLICA_CATCH_UP_MS = 10_000;

const row = (page: Page, name: string) => page.locator(`[data-token-row="${name}"]`);
const chip = (page: Page, key: string) => page.locator(`[data-token-spaces-picker] [data-space="${key}"]`);

/** Calls the API the way a script would: Node's own fetch, the token as a bearer, no cookies. */
function asScript(secret: string, path: string, init: RequestInit = {}): Promise<Response> {
  return fetch(`${WEB_URL}/api/v1${path}`, {
    ...init,
    headers: { Authorization: `Bearer ${secret}`, "Content-Type": "application/json", ...init.headers },
  });
}

/** Two spaces of the test's own: the one the token is given, and one it is not. */
async function twoSpaces(api: StatorApi, testInfo: TestInfo): Promise<{ near: string; far: string }> {
  const stem = uniqueKey(testInfo).slice(0, 9);
  const near = `${stem}N`;
  const far = `${stem}F`;
  await createSpace(api, near, `Near ${stem}`);
  await createSpace(api, far, `Far ${stem}`);
  return { near, far };
}

/** Opens the tokens page once the spaces are there to pick. */
async function openWith(page: Page, key: string): Promise<void> {
  await expect(async () => {
    await page.goto(TOKENS_PATH);
    await expect(chip(page, key)).toBeVisible({ timeout: 1_000 });
  }).toPass();
}

test.describe("tokens limited to spaces", { tag: ["@auth"] }, () => {
  test.afterEach(async ({ api }) => {
    const { data } = await api.GET("/tokens");
    for (const token of data?.tokens ?? []) await api.DELETE("/tokens/{tokenID}", { params: { path: { tokenID: token.id } } });
  });

  test("a token is limited to a space, says so, and reaches nothing else", async ({ page, api }, testInfo) => {
    const { near, far } = await twoSpaces(api, testInfo);
    const name = uniqueName(testInfo, "pipeline");
    await openWith(page, near);

    const picker = page.getByRole("group", { name: "Spaces" });
    await expect(picker).toContainText("Every space you can reach.");
    await chip(page, near).click();
    await expect(chip(page, near)).toHaveAttribute("aria-pressed", "true");
    await expect(chip(page, far)).toHaveAttribute("aria-pressed", "false");
    await expect(picker).toContainText("This token reaches only the spaces picked here");

    await page.getByLabel("Token name", { exact: true }).fill(name);
    await page.locator('[data-action="create-token"]').click();
    const secret = await page.getByLabel("Your new token", { exact: true }).inputValue();
    await expect(chip(page, near)).toHaveAttribute("aria-pressed", "false");
    await expect(row(page, name).locator("[data-token-spaces]")).toHaveText(`Only ${near}`);
    await expect(row(page, name).locator("[data-token-spaces]")).toHaveAttribute("data-token-spaces", near);

    await expect(async () => {
      expect((await asScript(secret, `/spaces/${near}`)).status).toBe(200);
    }).toPass({ timeout: REPLICA_CATCH_UP_MS });
    const listed = (await (await asScript(secret, "/spaces")).json()) as { spaces: { key: string }[] };
    expect(listed.spaces.map((space) => space.key)).toEqual([near]);
    expect((await asScript(secret, `/spaces/${far}`)).status).toBe(404);
    expect((await api.GET("/spaces/{spaceKey}", { params: { path: { spaceKey: far } } })).response.status).toBe(200);

    const made = await asScript(secret, "/spaces", { method: "POST", body: JSON.stringify({ key: `${near.slice(0, 9)}X`, name: "Not allowed" }) });
    expect(made.status).toBe(403);
    const refusal = (await made.json()) as { error: { code: string; message: string } };
    expect(refusal.error.code).toBe("spaces_token");
    expect(refusal.error.message).toContain("limited to some spaces");
  });

  test("the spaces are picked from the keyboard", { tag: ["@desktop"] }, async ({ page, api }, testInfo) => {
    const { near } = await twoSpaces(api, testInfo);
    const name = uniqueName(testInfo, "keys");
    await openWith(page, near);
    await page.getByLabel("Token name", { exact: true }).fill(name);
    await chip(page, near).focus();
    await page.keyboard.press("Space");
    await expect(chip(page, near)).toHaveAttribute("aria-pressed", "true");
    await page.keyboard.press("Enter");
    await expect(chip(page, near)).toHaveAttribute("aria-pressed", "false");
    await page.keyboard.press("Space");
    await page.getByLabel("Token name", { exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(page.getByLabel("Your new token", { exact: true })).toBeVisible();
    await expect(row(page, name).locator("[data-token-spaces]")).toHaveText(`Only ${near}`);
  });

  for (const scheme of ["light", "dark"] as ColourScheme[]) {
    test(`the picker and a limited token pass axe in ${scheme}`, async ({ page, api }, testInfo) => {
      const { near } = await twoSpaces(api, testInfo);
      const made = await api.POST("/tokens", { body: { name: uniqueName(testInfo, `axe-${scheme}`), spaces: [near] } });
      expect(made.response.status).toBe(201);
      await startInScheme(page, scheme);
      await openWith(page, near);
      await chip(page, near).click();
      await expect(chip(page, near)).toHaveAttribute("aria-pressed", "true");
      await expectAccessible(page);
    });
  }

  test("on a phone the picker wraps and the page never scrolls sideways", { tag: ["@mobile"] }, async ({ page, api }, testInfo) => {
    const { near, far } = await twoSpaces(api, testInfo);
    await openWith(page, near);
    await expect(chip(page, far)).toBeVisible();
    await chip(page, far).click();
    await expect(chip(page, far)).toHaveAttribute("aria-pressed", "true");
    await expect(page.locator('[data-action="create-token"]')).toBeVisible();
    expect(await scrollsSideways(page)).toBe(false);
  });
});
