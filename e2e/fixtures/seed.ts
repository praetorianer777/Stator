import type { TestInfo } from "@playwright/test";
import type { components } from "../../web/src/api/schema";
import { createStatorApi, must, type StatorApi } from "./api";
import { authAvailable, stateFile } from "./auth";

export type Theme = components["schemas"]["Theme"];
export type ThemeSpec = components["schemas"]["Spec"];

/** A name no other test, worker or earlier run has used, so tests can share an organisation. */
export function uniqueName(testInfo: TestInfo, label: string): string {
  return `e2e ${testInfo.workerIndex}-${Date.now().toString(36)} ${label}`;
}

/** A theme that overrides nothing; pass what the test needs on top. */
export function themeSpec(overrides: Partial<ThemeSpec> = {}): ThemeSpec {
  return { colors: { light: {}, dark: {} }, fonts: {}, shape: {}, shadows: {}, cursors: {}, icons: {}, css: "", ...overrides };
}

export async function createTheme(api: StatorApi, name: string, spec: ThemeSpec = themeSpec(), shared = false): Promise<Theme> {
  return must(await api.POST("/themes", { body: { name, shared, spec } })).theme;
}

export async function updateTheme(api: StatorApi, id: string, body: { name?: string; shared?: boolean; spec?: ThemeSpec }): Promise<Theme> {
  return must(await api.PATCH("/themes/{themeID}", { params: { path: { themeID: id } }, body })).theme;
}

export async function listThemes(api: StatorApi): Promise<Theme[]> {
  return must(await api.GET("/themes")).themes;
}

/** Uses a theme, or with null goes back to whatever the organisation shows. */
export async function chooseTheme(api: StatorApi, themeId: string | null): Promise<void> {
  must(await api.PUT("/themes/active", { body: { themeId } }));
}

export async function uploadThemeAsset(api: StatorApi, themeId: string, name: string, type: string, content: string) {
  const form = new FormData();
  form.append("file", new Blob([content], { type }), name);
  // The generated type describes the multipart fields as strings; the form itself is what is sent.
  const body = form as unknown as { file: string };
  return must(await api.POST("/themes/{themeID}/assets", { params: { path: { themeID: themeId } }, body })).asset;
}

/**
 * Leaves demo with no default theme and nothing chosen by alice or bob, and
 * checks it: specs there judge the built-in look, contrast included, so a theme
 * any run left behind must never reach them. Theme specs use their own organisation.
 */
export async function clearDemoThemes(): Promise<void> {
  if (!(await authAvailable())) return;
  const alice = await createStatorApi(stateFile("alice"));
  const bob = await createStatorApi(stateFile("bob"));
  try {
    must(await alice.PUT("/themes/default", { body: { themeId: null } }));
    for (const api of [alice, bob]) {
      await chooseTheme(api, null);
      const active = must(await api.GET("/themes/active"));
      if (active.theme !== null) throw new Error(`demo still shows the theme ${active.theme.name} after clearing it.`);
    }
  } finally {
    await alice.dispose();
    await bob.dispose();
  }
}
