import { afterEach, describe, expect, it, vi } from "vitest";
import type * as ThemesApi from "@/api/themes";
import { cleanup, screen } from "@testing-library/react";
import { renderAt } from "@/test/app";

// The server is stood in for at the hooks, as in the themes' own tests: what
// is under test is the head each page draws, which waits on a theme to edit.
vi.mock("@/api/themes", async (importOriginal) => {
  const real = await importOriginal<typeof ThemesApi>();
  const theme = {
    id: "11111111-1111-1111-1111-111111111111",
    ownerId: "me",
    ownerName: "Me",
    name: "Magenta",
    shared: false,
    active: false,
    default: false,
    inUse: 0,
    createdAt: "2026-09-29T08:00:00Z",
    updatedAt: "2026-09-29T08:00:00Z",
    spec: real.emptySpec(),
    assets: [],
  };
  return {
    ...real,
    useActiveTheme: () => ({ data: { theme: null, source: "" } }),
    useThemes: () => ({ data: { themes: [] }, isLoading: false, error: null }),
    useTheme: (id: string | undefined) => ({ data: id ? { theme } : undefined, isLoading: false, error: null }),
    useThemeExamples: () => ({ data: { examples: [] } }),
  };
});

afterEach(cleanup);

describe("every route", () => {
  it.each([
    ["/", "Home"],
    ["/spaces", "Spaces"],
    ["/search", "Search"],
    ["/no/such/page", "Page not found"],
    ["/settings/themes", "Themes"],
    ["/settings/themes/new", "New theme"],
    ["/settings/themes/11111111-1111-1111-1111-111111111111", "Magenta"],
    ["/dev/editor", "Editor"],
  ])("%s has exactly one h1", async (path, title) => {
    await renderAt(path);
    await screen.findByRole("heading", { level: 1, name: title });
    expect(document.querySelectorAll("h1")).toHaveLength(1);
  });
});
