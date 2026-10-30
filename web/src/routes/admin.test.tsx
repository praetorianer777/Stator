import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Me } from "@/api/auth";
import { adminNavigation, arrival, renderAt, setViewportWidth, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { movedAddress } from "./admin";

afterEach(() => {
  vi.unstubAllGlobals();
  setViewportWidth(1280);
});

const member: Me = { ...signedIn, organization: { ...signedIn.organization!, role: "member", administers: false } };
const notAdministering = { "GET /access/me": { status: 200, body: { can: { use: true, createSpace: false, administer: false } } } };

const SECTIONS = {
  people: ["Installation", "Access", "Users"],
  work: ["Hub", "Templates", "Stale pages", "Example space"],
  integrations: ["Armature", "Exciter", "Connected apps", "Webhooks"],
  records: ["Audit log", "Export and import"],
};

const ADDRESSES: Record<string, string> = {
  Installation: "/admin/installation",
  Access: "/admin/access/roles",
  Users: "/admin/users",
  Hub: "/admin/hub",
  Templates: "/admin/templates",
  "Stale pages": "/admin/stale",
  "Example space": "/admin/example-space",
  Armature: "/admin/armature",
  Exciter: "/admin/exciter",
  "Connected apps": "/admin/connected-apps",
  Webhooks: "/admin/webhooks",
  "Audit log": "/admin/audit",
  "Export and import": "/admin/transfer",
};

describe("the Admin page", () => {
  it("lists every page of the installation in its four sections, each with what it is for", async () => {
    await renderAt("/admin");
    for (const [section, pages] of Object.entries(SECTIONS)) {
      const block = document.querySelector<HTMLElement>(`[data-admin-section="${section}"]`)!;
      const rows = within(block).getAllByRole("link");
      expect(rows.map((row) => row.querySelector("span > span")?.textContent)).toEqual(pages);
      for (const row of rows) expect(row.textContent?.length).toBeGreaterThan(30);
    }
    expect(screen.getByRole("region", { name: "People and access" })).toHaveAttribute("data-admin-section", "people");
    expect(await axeViolations()).toEqual([]);
  });

  it("has its sections beside it, which lead to every page", async () => {
    const router = await renderAt("/admin");
    const nav = await adminNavigation();
    for (const [label, address] of Object.entries(ADDRESSES)) {
      await userEvent.click(within(nav).getByRole("link", { name: new RegExp(`^${label}`) }));
      await waitFor(() => expect(router.state.location.pathname).toBe(address));
    }
    await userEvent.click(within(nav).getByRole("link", { name: "All sections" }));
    await arrival(router, "/admin");
  });

  it("puts its sections behind one menu on a narrow screen", async () => {
    setViewportWidth(400);
    const router = await renderAt("/admin");
    await screen.findByRole("button", { name: "Admin" });
    expect(screen.queryByRole("navigation", { name: "Admin sections" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Admin" }));
    const menu = screen.getByRole("menu", { name: "Section" });
    expect(within(menu).getByRole("group", { name: "Integrations" })).toBeInTheDocument();
    await userEvent.click(within(menu).getByRole("menuitem", { name: "Webhooks" }));
    await arrival(router, "/admin/webhooks");
    expect(screen.getByRole("button", { name: "Webhooks" })).toBeInTheDocument();
  });

  it("is reached from one Admin entry in the sidebar, and its crumb leads back", async () => {
    stubApi({ "GET /webhooks": { status: 200, body: { webhooks: [] } } });
    const router = await renderAt("/admin/webhooks");
    const crumbs = await screen.findByRole("navigation", { name: "Breadcrumb" });
    await userEvent.click(within(crumbs).getByRole("link", { name: "Admin" }));
    await arrival(router, "/admin");
    await router.navigate({ to: "/tasks" });
    const everywhere = screen.getAllByRole("navigation", { name: "Where you are" })[0]!;
    await userEvent.click(within(everywhere).getByRole("link", { name: /^Admin/ }));
    await arrival(router, "/admin");
  });
});

describe("somebody who does not administer the installation", () => {
  it.each(["/admin", "/admin/users", "/admin/access/members", "/admin/connected-apps", "/admin/audit"])(
    "is refused once at %s, for the whole of Admin, and nothing of its pages is asked for",
    async (path) => {
      const sent = stubApi(notAdministering);
      await renderAt(path, { me: member });
      expect(await screen.findByText("Only administrators can open this page")).toBeInTheDocument();
      expect(screen.queryByRole("navigation", { name: "Admin sections" })).toBeNull();
      expect(document.querySelectorAll("[data-admin-refused]")).toHaveLength(1);
      expect(sent.filter((r) => /^\/(users|roles|groups|webhooks|audit|oauth|site\/tokens|oidc-provider)/.test(r.path))).toEqual([]);
    },
  );

  it("finds no Admin entry in the sidebar or the account menu", async () => {
    stubApi(notAdministering);
    await renderAt("/tasks", { me: member });
    await screen.findAllByRole("navigation", { name: "Where you are" });
    await waitFor(() => expect(screen.queryByRole("link", { name: /^Admin/ })).toBeNull());
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    expect(screen.queryByRole("menuitem", { name: "Admin" })).toBeNull();
  });
});

describe("the addresses of before", () => {
  it.each([
    ["/settings/access", "/admin/access/roles"],
    ["/settings/access/groups", "/admin/access/groups"],
    ["/settings/sso", "/admin/access/sso"],
    ["/settings/permissions", "/admin/access/roles"],
    ["/settings/brand", "/admin/installation"],
    ["/settings/hub", "/admin/hub"],
    ["/settings/templates", "/admin/templates"],
    ["/settings/stale", "/admin/stale"],
    ["/settings/example-space", "/admin/example-space"],
    ["/settings/armature", "/admin/armature"],
    ["/settings/exciter", "/admin/exciter"],
    ["/settings/webhooks", "/admin/webhooks"],
    ["/settings/audit", "/admin/audit"],
    ["/settings/transfer", "/admin/transfer"],
  ])("lead from %s to %s", async (from, to) => {
    const router = await renderAt(from);
    await waitFor(() => expect(router.state.location.pathname).toBe(to));
    await arrival(router, to);
  });

  it("keep their search and their section", () => {
    expect(movedAddress("armature", "", {}, "?armature=connected", "")).toBe("/admin/armature?armature=connected");
    expect(movedAddress("audit", "", {}, "", "top")).toBe("/admin/audit#top");
    expect(movedAddress("templates", "meeting-notes")).toBe("/admin/templates/meeting-notes");
    expect(movedAddress("profile")).toBeUndefined();
  });

  it("send a space's stale pages and new template below the space's settings", () => {
    expect(movedAddress("stale", "", { space: "docs" })).toBe("/s/DOCS/settings/stale");
    expect(movedAddress("templates", "new", { space: "DOCS" })).toBe("/s/DOCS/settings/templates/new");
  });

  it("leave the person's own pages where they are, and find nothing at an address that never was", async () => {
    const router = await renderAt("/settings/themes");
    await arrival(router, "/settings/themes");
    await router.navigate({ to: "/settings/$section", params: { section: "nowhere" } });
    expect(await screen.findByText("Page not found")).toBeInTheDocument();
  });
});
