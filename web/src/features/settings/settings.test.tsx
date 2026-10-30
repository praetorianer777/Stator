import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { renderAt, settingsSidebar, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const member: Me = { ...signedIn, organization: { ...signedIn.organization!, role: "member", administers: false } };
const guest: Me = {
  ...signedIn,
  organization: { ...signedIn.organization!, role: "guest", administers: false, guestSpace: { id: "s-eng", key: "ENG", name: "Engineering" } },
};
const notAdministering = { "GET /access/me": { status: 200, body: { can: { use: true, createSpace: false, administer: false } } } };

const YOU = ["Your profile", "Notification settings", "Watching", "Themes", "Access tokens", "Armature account", "Exciter account"];
function linksOf(group: string) {
  const panel = document.querySelector<HTMLElement>(`[data-sidebar-group="${group}"]`);
  return panel
    ? within(panel)
        .getAllByRole("link")
        .map((link) => link.textContent)
    : [];
}

describe("the settings area", () => {
  it("trades the wiki's sidebar for its own, with the person's own pages alone, an administrator's too", async () => {
    await renderAt("/settings/profile");
    const sidebar = await settingsSidebar();
    expect(within(sidebar).queryByRole("button", { name: "Wiki" })).toBeNull();
    expect(linksOf("settings-you")).toEqual(YOU);
    expect(document.querySelector('[data-sidebar-group="settings-site"]')).toBeNull();
    expect(within(sidebar).queryByRole("link", { name: /Webhooks|Access$|Admin/ })).toBeNull();
    expect(within(sidebar).getByRole("link", { name: "Your profile" })).toHaveAttribute("data-status", "active");
    expect(within(sidebar).getByRole("link", { name: "Armature account" })).toHaveAttribute("href", "/settings/profile#armature");
    expect(within(sidebar).getByRole("link", { name: "Armature account" })).not.toHaveAttribute("data-status", "active");
  });

  it("leaves Access tokens out for a guest", async () => {
    stubApi(notAdministering);
    await renderAt("/settings/profile", { me: guest });
    await settingsSidebar();
    await waitFor(() => expect(linksOf("settings-you")).toEqual(YOU.filter((name) => name !== "Access tokens")));
  });

  it("goes back to the last page outside it", async () => {
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [] } }, "GET /search": { status: 200, body: { results: [], next: null, total: 0 } } });
    const router = await renderAt("/spaces");
    await screen.findByText("No spaces yet");
    await router.navigate({ to: "/settings/themes" });
    await router.navigate({ to: "/settings/watching" });
    await userEvent.click(within(await settingsSidebar()).getByRole("link", { name: "Back to Stator" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/spaces"));
    expect(document.querySelector('[data-sidebar-group="wiki"]')).not.toBeNull();
  });
});

describe("the settings hub", () => {
  it("lists the person's own pages, each with what it is for, and nothing of the installation", async () => {
    await renderAt("/settings");
    const you = await screen.findByRole("region", { name: "You" });
    const rows = within(you).getAllByRole("link");
    expect(rows.map((link) => link.querySelector("span > span")?.textContent)).toEqual(YOU);
    for (const row of rows) expect(row.querySelectorAll("span > span")[1]?.textContent).toMatch(/\w+.*\.$/);
    expect(within(you).getByRole("link", { name: /^Themes/ })).toHaveTextContent("How Stator looks to you");
    expect(screen.queryByRole("link", { name: /^Webhooks/ })).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("offers a member the same pages", async () => {
    stubApi(notAdministering);
    await renderAt("/settings", { me: member });
    const you = await screen.findByRole("region", { name: "You" });
    expect(within(you).getAllByRole("link")).toHaveLength(YOU.length);
  });

  it("is where the Settings crumb of a settings page leads", async () => {
    const router = await renderAt("/settings/watching");
    const crumbs = await screen.findByRole("navigation", { name: "Breadcrumb" });
    await userEvent.click(within(crumbs).getByRole("link", { name: "Settings" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings"));
  });
});

describe("the palette", () => {
  it("offers an administrator each Admin page as Admin / <page>, beside the pages", async () => {
    stubApi({ "GET /recent-pages": { status: 200, body: { pages: [] } }, "GET /search/quick": { status: 200, body: { pages: [] } } });
    const router = await renderAt("/");
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.type(screen.getByRole("combobox"), "web");
    const places = await screen.findByRole("listbox", { name: "Go to" });
    const options = within(places).getAllByRole("option");
    expect(options.map((option) => option.getAttribute("data-quick-search-place"))).toEqual(["webhooks"]);
    expect(options[0]).toHaveTextContent("Admin / Webhooks");
    await userEvent.keyboard("{ArrowDown}{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/webhooks"));
  });

  it("offers an administrator Admin itself", async () => {
    stubApi({ "GET /recent-pages": { status: 200, body: { pages: [] } }, "GET /search/quick": { status: 200, body: { pages: [] } } });
    await renderAt("/");
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.type(screen.getByRole("combobox"), "admin");
    const places = await screen.findByRole("listbox", { name: "Go to" });
    expect(
      within(places)
        .getAllByRole("option")
        .map((option) => option.getAttribute("data-quick-search-place")),
    ).toContain("admin");
  });

  it("offers a member nothing of Admin", async () => {
    stubApi({
      ...notAdministering,
      "GET /recent-pages": { status: 200, body: { pages: [] } },
      "GET /search/quick": { status: 200, body: { pages: [] } },
    });
    await renderAt("/", { me: member });
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.type(screen.getByRole("combobox"), "the");
    const places = await screen.findByRole("listbox", { name: "Go to" });
    expect(
      within(places)
        .getAllByRole("option")
        .map((option) => option.getAttribute("data-quick-search-place")),
    ).toEqual(["themes"]);
    await userEvent.clear(screen.getByRole("combobox"));
    await userEvent.type(screen.getByRole("combobox"), "web");
    await waitFor(() => expect(screen.queryByRole("listbox", { name: "Go to" })).toBeNull());
  });
});
