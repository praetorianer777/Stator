import type { TestInfo } from "@playwright/test";
import type { components } from "../../web/src/api/schema";
import { must, type StatorApi } from "./api";

export type Theme = components["schemas"]["Theme"];
export type ThemeSpec = components["schemas"]["Spec"];

/** A name no other test, worker or earlier run has used, so specs can share an organisation. */
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

/** Deletes every theme of the API's user whose name passes the test; what is not theirs is left alone. */
export async function deleteThemes(api: StatorApi, matches: (name: string) => boolean): Promise<void> {
  const themes = (await api.GET("/themes")).data?.themes ?? [];
  for (const theme of themes.filter((each) => matches(each.name))) {
    await api.DELETE("/themes/{themeID}", { params: { path: { themeID: theme.id } } });
  }
}
