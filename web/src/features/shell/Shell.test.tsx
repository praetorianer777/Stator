import { beforeEach, describe, expect, it } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryHistory } from "@tanstack/react-router";
import { buildRouter } from "@/routes";
import { THEME_STORAGE_KEY } from "@/lib/theme";
import { GROUPS_KEY, SIDEBAR_KEY } from "./state";

// The real router and the real shell, started at a path; nothing below the
// shell is stood in for.
async function renderAt(path: string) {
  const queryClient = new QueryClient();
  const router = buildRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(document.querySelector("[data-rail]")).not.toBeNull());
  return router;
}

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});

describe("the shell", () => {
  it("draws the rail, the sidebar and the top bar around the page", async () => {
    await renderAt("/");
    const rail = document.querySelector<HTMLElement>("[data-rail]")!;
    const sidebar = document.querySelector<HTMLElement>('[data-sidebar="open"]')!;
    expect(sidebar).not.toBeNull();
    for (const place of [rail, sidebar]) {
      expect(within(place).getByRole("link", { name: /Home/ })).toHaveAttribute("href", "/");
      expect(within(place).getByRole("link", { name: /Spaces/ })).toHaveAttribute("href", "/spaces");
    }
    expect(document.querySelector("[data-top-bar]")).not.toBeNull();
    expect(screen.getByRole("button", { name: "Your account" })).toBeInTheDocument();
    expect(await screen.findByText("Welcome to Stator")).toBeInTheDocument();
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
    await renderAt("/spaces");
    expect(await screen.findByText("No spaces yet")).toBeInTheDocument();
    const sidebar = document.querySelector<HTMLElement>("[data-sidebar]")!;
    expect(within(sidebar).getByRole("link", { name: /Spaces/ })).toHaveAttribute("data-status", "active");
    expect(within(sidebar).getByRole("link", { name: /Home/ })).not.toHaveAttribute("data-status", "active");
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
  it("opens from the top bar and from Ctrl K", async () => {
    const router = await renderAt("/");
    await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="search"]')!);
    await waitFor(() => expect(router.state.location.pathname).toBe("/search"));

    await act(() => router.navigate({ to: "/" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    await userEvent.keyboard("{Control>}k{/Control}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/search"));
    expect(await screen.findByText("Search is on its way")).toBeInTheDocument();
  });
});

describe("a path that leads nowhere", () => {
  it("says so inside the shell and offers the way home", async () => {
    const router = await renderAt("/no/such/page");
    expect(await screen.findByText("Page not found")).toBeInTheDocument();
    expect(document.querySelector("[data-not-found]")).not.toBeNull();
    expect(document.querySelector("[data-sidebar]")).not.toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "Go home" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(await screen.findByText("Welcome to Stator")).toBeInTheDocument();
  });
});
