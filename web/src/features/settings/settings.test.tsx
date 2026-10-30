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
const SITE = [
  "Access",
  "Hub page",
  "Templates",
  "Stale pages",
  "Example space",
  "Export brand",
  "Export and import",
  "Armature",
  "Exciter",
  "Webhooks",
  "Audit log",
];

function linksOf(group: string) {
  const panel = document.querySelector<HTMLElement>(`[data-sidebar-group="${group}"]`);
  return panel
    ? within(panel)
        .getAllByRole("link")
        .map((link) => link.textContent)
    : [];
}

describe("the settings area", () => {
  it("trades the wiki's sidebar for its own, with the person's pages and the organization's for a site administrator", async () => {
    await renderAt("/settings/profile");
    const sidebar = await settingsSidebar();
    expect(within(sidebar).queryByRole("button", { name: "Wiki" })).toBeNull();
    expect(linksOf("settings-you")).toEqual(YOU);
    expect(linksOf("settings-site")).toEqual(SITE);
    expect(within(sidebar).getByRole("link", { name: "Your profile" })).toHaveAttribute("data-status", "active");
    expect(within(sidebar).getByRole("link", { name: "Armature account" })).toHaveAttribute("href", "/settings/profile#armature");
    expect(within(sidebar).getByRole("link", { name: "Armature account" })).not.toHaveAttribute("data-status", "active");
  });

  it("leaves the organization's pages out for a member", async () => {
    stubApi(notAdministering);
    await renderAt("/settings/profile", { me: member });
    await settingsSidebar();
    await waitFor(() => expect(document.querySelector('[data-sidebar-group="settings-site"]')).toBeNull());
    expect(linksOf("settings-you")).toEqual(YOU);
  });

  it("leaves Access tokens out for a guest", async () => {
    stubApi(notAdministering);
    await renderAt("/settings/profile", { me: guest });
    await settingsSidebar();
    await waitFor(() => expect(document.querySelector('[data-sidebar-group="settings-site"]')).toBeNull());
    expect(linksOf("settings-you")).toEqual(YOU.filter((name) => name !== "Access tokens"));
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
  it("offers a site administrator a card of their own pages and one of the organization's", async () => {
    await renderAt("/settings");
    const you = await screen.findByRole("region", { name: "You" });
    expect(
      within(you)
        .getAllByRole("link")
        .map((link) => link.textContent),
    ).toEqual(YOU);
    const site = screen.getByRole("region", { name: "Settings" });
    expect(
      within(site)
        .getAllByRole("link")
        .map((link) => link.textContent),
    ).toEqual(SITE);
    expect(within(site).getByRole("link", { name: "Webhooks" })).toHaveAttribute("href", "/settings/webhooks");
    expect(await axeViolations()).toEqual([]);
  });

  it("offers a member only their own", async () => {
    stubApi(notAdministering);
    await renderAt("/settings", { me: member });
    await screen.findByRole("region", { name: "You" });
    await waitFor(() => expect(screen.queryByRole("region", { name: "Settings" })).toBeNull());
  });

  it("is where the Settings crumb of a settings page leads", async () => {
    const router = await renderAt("/settings/watching");
    const crumbs = await screen.findByRole("navigation", { name: "Breadcrumb" });
    await userEvent.click(within(crumbs).getByRole("link", { name: "Settings" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings"));
  });
});

describe("the palette", () => {
  it("offers the places a site administrator may go, beside the pages", async () => {
    stubApi({ "GET /recent-pages": { status: 200, body: { pages: [] } }, "GET /search/quick": { status: 200, body: { pages: [] } } });
    const router = await renderAt("/");
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.type(screen.getByRole("combobox"), "web");
    const places = await screen.findByRole("listbox", { name: "Go to" });
    expect(
      within(places)
        .getAllByRole("option")
        .map((option) => option.getAttribute("data-quick-search-place")),
    ).toEqual(["webhooks"]);
    await userEvent.keyboard("{ArrowDown}{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/webhooks"));
  });

  it("offers a member none of the organization's", async () => {
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
