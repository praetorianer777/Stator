import type { APIRequestContext, Page } from "@playwright/test";
import { must } from "../fixtures/api";
import { expect } from "../fixtures/auth";
import { orgTest as test } from "../fixtures/org";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { armatureURL } from "../fixtures/stack";

// The stub shows each person one of Stator's example themes, as Armature
// would show the theme they picked there.
const patFor = (tenant: string, person: string) => `armature_pat_${tenant}_${person}`;
const SCHEMES: ColourScheme[] = ["light", "dark"];
// The light accents of the two examples, as the stylesheet sets them.
const DEEP_TECH_ACCENT = "#2f6fd6";
const CONSTELLATION_ACCENT = "#1f7fb8";

const customStyle = (page: Page) => page.locator("style#stator-theme");
const rootVar = (page: Page, name: string) => page.evaluate((n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim(), name);
const followSwitch = (page: Page) => page.getByRole("switch", { name: "Follow my Armature theme" });
const note = (page: Page) => page.locator("[data-armature-theme-note]");

async function stub(request: APIRequestContext, path: string, data: unknown) {
  const answer = await request.fetch(`${armatureURL()}/_stub/${path}`, {
    method: "PUT",
    data,
  });
  expect(answer.ok(), `the stub answered PUT ${path} with ${answer.status()}`).toBe(true);
}

test.describe("following the Armature theme", { tag: ["@auth"] }, () => {
  test.beforeEach(async ({ api, freshOrg, request }) => {
    await request.fetch(`${armatureURL()}/_stub/${freshOrg.slug}`, {
      method: "DELETE",
    });
    await api.DELETE("/armature/connection");
    must(
      await api.PUT("/armature/connection", {
        body: { baseUrl: armatureURL(), orgSlug: freshOrg.slug },
      }),
    );
    must(
      await api.PUT("/armature/account/token", {
        body: { token: patFor(freshOrg.slug, "alice") },
      }),
    );
    await stub(request, `${freshOrg.slug}/people/alice/theme`, {
      theme: "deep-tech",
    });
  });

  test.afterEach(async ({ api }) => {
    await api.DELETE("/armature/theme");
    await api.DELETE("/armature/connection");
  });

  test("alice follows her Armature theme, and a change there reaches the page", async ({ page, freshOrg, request }) => {
    await startInScheme(page, "light");
    await page.goto("/settings/themes");
    await expect(followSwitch(page)).toHaveAttribute("aria-checked", "false");
    await expect(customStyle(page)).toHaveCount(0);

    await followSwitch(page).click();
    await expect(followSwitch(page)).toHaveAttribute("aria-checked", "true");
    await expect(note(page)).toContainText("Stator shows the theme you use in Armature");
    await expect(page.locator("main")).toContainText("You are using Deep-Tech, your theme in Armature.");
    await expect.poll(() => rootVar(page, "--color-accent")).toBe(DEEP_TECH_ACCENT);

    // The theme stays on across a reload, painted before the server answers.
    await page.reload();
    await expect.poll(() => rootVar(page, "--color-accent")).toBe(DEEP_TECH_ACCENT);

    // The settings ask Armature now, so the change shows without waiting for the cache.
    await stub(request, `${freshOrg.slug}/people/alice/theme`, {
      theme: "constellation",
    });
    await page.reload();
    await expect(page.locator("main")).toContainText("You are using Constellation, your theme in Armature.");
    await expect.poll(() => rootVar(page, "--color-accent")).toBe(CONSTELLATION_ACCENT);

    await followSwitch(page).click();
    await expect(followSwitch(page)).toHaveAttribute("aria-checked", "false");
    await expect(customStyle(page)).toHaveCount(0);
  });

  for (const scheme of SCHEMES) {
    test(`the follow setting is accessible in ${scheme}`, async ({ page, api, freshOrg, request }) => {
      // Armature's built-in theme, which Stator's matches, so axe judges
      // the setting rather than the colours of an example theme.
      await stub(request, `${freshOrg.slug}/people/alice/theme`, { theme: "" });
      await startInScheme(page, scheme);
      must(await api.PUT("/armature/theme"));
      await page.goto("/settings/themes");
      await expect(followSwitch(page)).toHaveAttribute("aria-checked", "true");
      await expect(page.locator("main")).toContainText("You are using the built-in theme, as you do in Armature.");
      await expect(customStyle(page)).toHaveCount(0);
      await expectAccessible(page);

      // A theme Stator cannot use says why, and the page falls back.
      await stub(request, `${freshOrg.slug}/people/alice/theme`, {
        theme: "constellation",
        broken: true,
      });
      await page.reload();
      await expect(note(page)).toContainText("Armature's theme cannot be used in Stator");
      await expect(customStyle(page)).toHaveCount(0);
      await expectAccessible(page);
    });
  }
});
