import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { THEME_STORAGE_KEY } from "@/lib/theme";
import { renderAt, stubApi } from "@/test/app";
import { LAST_SPACE_KEY } from "@/config";
import { aSpace } from "@/test/spaces";
import { GROUPS_KEY, SIDEBAR_KEY } from "./state";

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});

afterEach(() => vi.unstubAllGlobals());

describe("the shell", () => {
  it("draws the rail and the sidebar around the page, with no top bar", async () => {
    await renderAt("/");
    const rail = document.querySelector<HTMLElement>("[data-rail]")!;
    const sidebar = document.querySelector<HTMLElement>('[data-sidebar="open"]')!;
    expect(sidebar).not.toBeNull();
    for (const place of [rail, sidebar]) {
      expect(within(place).getByRole("link", { name: /Home/ })).toHaveAttribute("href", "/");
      expect(within(place).getByRole("link", { name: /Spaces/ })).toHaveAttribute("href", "/spaces");
    }
    expect(document.querySelector("[data-top-bar]")).toBeNull();
    expect(within(rail).getByRole("button", { name: "Your account" })).toBeInTheDocument();
    expect(await screen.findByText("Welcome to Stator")).toBeInTheDocument();
  });

  it("names the installation beside its mark at the head of the sidebar", async () => {
    await renderAt("/");
    const head = document.querySelector<HTMLElement>('[data-sidebar="open"] [data-sidebar-head]')!;
    expect(head).toHaveTextContent("Demo");
    expect(head.querySelector("[data-logo-mark]")).not.toBeNull();
  });

  it("keeps the rail's toggle inside its navigation", async () => {
    await renderAt("/");
    const everywhere = within(document.querySelector<HTMLElement>("[data-rail]")!).getByRole("navigation", { name: "Everywhere" });
    expect(within(everywhere).getByRole("button", { name: "Collapse the sidebar" })).toBeInTheDocument();
  });

  it("lists the rail in its order, the bell after Search and the avatar at the bottom", async () => {
    await renderAt("/");
    const rail = document.querySelector<HTMLElement>("[data-rail]")!;
    const everywhere = within(rail).getByRole("navigation", { name: "Everywhere" });
    const names = Array.from(everywhere.querySelectorAll<HTMLElement>("[data-nav], [data-action]")).map(
      (el) => el.getAttribute("data-nav") ?? el.getAttribute("data-action"),
    );
    expect(names).toEqual(["sidebar", "home", "search", "notifications", "spaces", "tasks"]);
    const bottom = rail.lastElementChild as HTMLElement;
    expect(within(bottom).getByRole("button", { name: "Auto" })).toBeInTheDocument();
    expect(within(bottom).getByRole("button", { name: "Your account" })).toBeInTheDocument();
  });

  it("lists the wiki's places in the sidebar, Reviews among them", async () => {
    await renderAt("/");
    const wiki = document.querySelector<HTMLElement>('[data-sidebar-group="wiki"]')!;
    const names = Array.from(wiki.querySelectorAll<HTMLElement>("[data-nav]")).map((el) => el.getAttribute("data-nav"));
    expect(names).toEqual(["home", "search", "notifications", "reviews", "tasks", "spaces"]);
    expect(within(wiki).getByRole("link", { name: /Reviews/ })).toHaveAttribute("href", "/reviews");
  });

  it("folds the sidebar to the rail and remembers it", async () => {
    await renderAt("/");
    await userEvent.click(screen.getByRole("button", { name: "Collapse the sidebar" }));
    expect(document.querySelector("[data-sidebar]")).toBeNull();
    expect(document.querySelector("[data-rail]")).not.toBeNull();
    expect(localStorage.getItem(SIDEBAR_KEY)).toBe("rail");

    await userEvent.click(screen.getByRole("button", { name: "Expand the sidebar" }));
    expect(document.querySelector('[data-sidebar="open"]')).not.toBeNull();
    expect(localStorage.getItem(SIDEBAR_KEY)).toBe("open");
  });

  it("folds a sidebar group by its title and remembers it", async () => {
    await renderAt("/");
    await userEvent.click(screen.getByRole("button", { name: "Wiki" }));
    expect(document.querySelector('[data-sidebar-group="wiki"]')).toHaveAttribute("data-open", "false");
    expect(JSON.parse(localStorage.getItem(GROUPS_KEY) ?? "{}")).toEqual({ wiki: false });
  });

  it("marks where the reader is", async () => {
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [] } } });
    await renderAt("/spaces");
    expect(await screen.findByText("No spaces yet")).toBeInTheDocument();
    const sidebar = document.querySelector<HTMLElement>("[data-sidebar]")!;
    expect(within(sidebar).getByRole("link", { name: /Spaces/ })).toHaveAttribute("data-status", "active");
    expect(within(sidebar).getByRole("link", { name: /Home/ })).not.toHaveAttribute("data-status", "active");
  });
});

describe("the build in the sidebar", () => {
  it("names the api's build, with the commit and build time on hover", async () => {
    stubApi({
      "GET /meta": {
        status: 200,
        body: {
          product: "stator",
          build: { version: "0.1.0+4.gabc1234", commit: "abc1234", builtAt: "2026-10-14T09:00:00Z" },
          apiVersion: "1",
          capabilities: [],
        },
      },
    });
    await renderAt("/");
    const sidebar = document.querySelector<HTMLElement>('[data-sidebar="open"]')!;
    await waitFor(() => expect(sidebar.querySelector('[data-build="api"]')).not.toBeNull());
    const stamp = sidebar.querySelector<HTMLElement>("[data-build]")!;
    expect(stamp).toHaveTextContent("v0.1.0+4 · abc1234");
    expect(stamp.title).toContain("Commit abc1234");
    expect(stamp.title).toMatch(/Built .*2026/);
  });

  it("names the client's own build while the api cannot say", async () => {
    stubApi({ "GET /meta": { status: 503, body: { error: { code: "unavailable", message: "Down." } } } });
    await renderAt("/");
    const stamp = document.querySelector<HTMLElement>('[data-sidebar="open"] [data-build]')!;
    expect(stamp).toHaveAttribute("data-build", "web");
    expect(stamp.textContent).toMatch(/^v\d+\.\d+\.\d+/);
  });
});

describe("the theme switch", () => {
  it("cycles Auto, Light, Dark and back, and persists each choice", async () => {
    await renderAt("/");
    const button = () => document.querySelector<HTMLButtonElement>('[data-action="theme"]')!;
    expect(button()).toHaveAccessibleName("Auto");

    await userEvent.click(button());
    expect(button()).toHaveAccessibleName("Light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");

    await userEvent.click(button());
    expect(button()).toHaveAccessibleName("Dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");

    await userEvent.click(button());
    expect(button()).toHaveAccessibleName("Auto");
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
  });

  it("starts from the stored choice", async () => {
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    await renderAt("/");
    expect(document.querySelector('[data-action="theme"]')).toHaveAccessibleName("Dark");
  });
});

describe("search", () => {
  it("opens quick search from Ctrl K, and Search in the sidebar opens the full search", async () => {
    stubApi({ "GET /recent-pages": { status: 200, body: { pages: [] } } });
    const router = await renderAt("/");
    await userEvent.keyboard("{Control>}k{/Control}");
    expect(screen.getByRole("dialog", { name: "Quick search" })).toBeInTheDocument();
    expect(screen.getByRole("combobox")).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "Quick search" })).toBeNull();

    const sidebar = document.querySelector<HTMLElement>("[data-sidebar]")!;
    await userEvent.click(within(sidebar).getByRole("link", { name: /Search/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/search"));
  });
});

describe("the space in the sidebar", () => {
  const docs = { key: "DOCS", name: "Handbook" };

  it("keeps the space visited last on a page that names none, with the switcher and one Settings entry", async () => {
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [aSpace(docs), aSpace({ key: "OPS", name: "Operations" })] } },
      "GET /spaces/DOCS": { status: 200, body: { space: aSpace(docs) } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    });
    localStorage.setItem(LAST_SPACE_KEY, "DOCS");
    const router = await renderAt("/tasks");
    const group = document.querySelector<HTMLElement>('[data-sidebar-group="space"]')!;
    await within(group).findByRole("link", { name: "Space home" });
    expect(within(group).getByRole("button", { name: "Handbook" })).toHaveAttribute("aria-expanded", "true");
    expect(within(group).queryByRole("button", { name: "Space admin" })).toBeNull();
    expect(within(group).getByRole("link", { name: "Settings" })).toHaveAttribute("href", "/spaces/DOCS/settings");
    for (const gone of ["Trash", "Archive", "Stale pages"]) expect(within(group).queryByRole("link", { name: gone })).toBeNull();

    await userEvent.click(within(group).getByRole("button", { name: "Switch space: Handbook" }));
    const menu = screen.getByRole("menu", { name: "Switch space" });
    await userEvent.type(within(document.body).getByRole("textbox", { name: "Filter spaces" }), "oper");
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((item) => item.textContent),
    ).toEqual(["OperationsOPS", "All spaces"]);
    await userEvent.click(within(menu).getByRole("menuitem", { name: /Operations/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/spaces/OPS"));
  });

  it("leads a reader who administers nothing to the space's settings too", async () => {
    const reader = aSpace({ ...docs, can: { ...aSpace(docs).can, deletePages: false, administer: false } });
    stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space: reader } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    });
    localStorage.setItem(LAST_SPACE_KEY, "DOCS");
    await renderAt("/tasks");
    const group = document.querySelector<HTMLElement>('[data-sidebar-group="space"]')!;
    await within(group).findByRole("link", { name: "Space home" });
    expect(within(group).getByRole("link", { name: "Settings" })).toHaveAttribute("href", "/spaces/DOCS/settings");
  });
});

describe("a path that leads nowhere", () => {
  it("says so inside the shell and offers the way home", async () => {
    // Answered as the API answers, so no request waits out the retries a
    // request without any answer gets.
    stubApi({});
    const router = await renderAt("/no/such/page");
    expect(await screen.findByText("Page not found")).toBeInTheDocument();
    expect(document.querySelector("[data-not-found]")).not.toBeNull();
    expect(document.querySelector("[data-sidebar]")).not.toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "Go home" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByText("Welcome to Stator")).toBeInTheDocument();
  });
});
