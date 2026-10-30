import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Assignment, Group, PermissionDescription, RoleDescription } from "@/api/access";
import { answerConfirm, renderAt, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const ids = {
  everyone: "0195f000-0000-7000-8000-00000000e0e0",
  writers: "0195f000-0000-7000-8000-0000000000a1",
  staff: "0195f000-0000-7000-8000-0000000000a2",
  ada: "u-ada",
  bob: "0195f000-0000-7000-8000-00000000b0b0",
};

const permissions: PermissionDescription[] = [
  { permission: "view", words: "View the space and its pages", scope: "space" },
  { permission: "addComments", words: "Comment on pages", scope: "space" },
  { permission: "addPages", words: "Add, edit and move pages", scope: "space" },
  { permission: "delete", words: "Move pages to the trash and back", scope: "space" },
  { permission: "administer", words: "Administer the space", scope: "space" },
  { permission: "use", words: "Use Stator", scope: "installation" },
  { permission: "createSpace", words: "Create spaces", scope: "installation" },
  { permission: "administerSite", words: "Administer Stator", scope: "installation" },
];
const builtIn = (role: string, name: string, held: RoleDescription["permissions"], orgWideOnly = false): RoleDescription => ({
  role,
  name,
  description: "",
  orgWideOnly,
  builtin: true,
  permissions: held,
  inUse: 1,
});
const roles: RoleDescription[] = [
  builtIn("administrator", "Administrator", ["view", "addComments", "addPages", "delete", "administer", "use", "createSpace", "administerSite"], true),
  builtIn("space_administrator", "Space administrator", ["view", "addComments", "addPages", "delete", "administer"]),
  builtIn("editor", "Editor", ["view", "addComments", "addPages", "delete"]),
  builtIn("commenter", "Commenter", ["view", "addComments"]),
  builtIn("viewer", "Viewer", ["view"]),
  { role: "member", name: "Member", description: "", orgWideOnly: true, builtin: false, permissions: ["use"], inUse: 1 },
  { role: "proofreader", name: "Proofreader", description: "", orgWideOnly: false, builtin: false, permissions: ["view"], inUse: 2 },
];
const space = aSpace();
const grants: Assignment[] = [
  { id: "g-1", role: "administrator", roleName: "Administrator", userId: ids.ada, userName: "Ada Lovelace", createdAt: "2026-10-01T08:00:00Z" },
  { id: "g-2", role: "member", roleName: "Member", groupId: ids.everyone, groupName: "Everyone", groupBuiltin: "everyone", createdAt: "2026-10-01T08:00:00Z" },
  {
    id: "g-3",
    role: "editor",
    roleName: "Editor",
    spaceId: space.id,
    spaceKey: "DOCS",
    spaceName: space.name,
    groupId: ids.writers,
    groupName: "Writers",
    createdAt: "2026-10-01T08:00:00Z",
  },
];
const groups: Group[] = [
  {
    id: ids.everyone,
    name: "Everyone",
    description: "",
    source: "local",
    externalRef: "",
    builtin: "everyone",
    memberCount: 9,
    roleCount: 1,
    fromProvider: false,
  },
  { id: ids.writers, name: "Writers", description: "", source: "local", externalRef: "", memberCount: 1, roleCount: 1, fromProvider: false },
  { id: ids.staff, name: "Staff", description: "", source: "oidc", externalRef: "staff", memberCount: 3, roleCount: 0, fromProvider: true },
];
const page = (assignments: Assignment[]) => ({ assignments, total: assignments.length, limit: 50, offset: 0 });

function stubAccess(more: Parameters<typeof stubApi>[0] = {}) {
  return stubApi({
    "GET /roles": { status: 200, body: { roles } },
    "GET /permissions": { status: 200, body: { permissions } },
    "GET /role-assignments": { status: 200, body: page(grants) },
    "GET /groups": { status: 200, body: { groups } },
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /people": { status: 200, body: { people: [{ id: ids.bob, name: "Bob Builder", email: "bob@stator.test" }] } },
    [`GET /groups/${ids.writers}`]: {
      status: 200,
      body: { group: { ...groups[1], members: [{ id: ids.ada, name: "Ada Lovelace", email: "ada@stator.test" }] } },
    },
    [`GET /groups/${ids.staff}`]: { status: 200, body: { group: { ...groups[2], members: [] } } },
    "GET /oidc-provider": { status: 200, body: { provider: null, callbackUrl: "http://localhost/api/v1/auth/oidc/callback" } },
    "GET /users/requests": { status: 200, body: { requests: [] } },
    ...more,
  });
}

describe("the Access page", () => {
  it("opens on its Roles tab, and each tab is an address of its own", async () => {
    stubAccess();
    const router = await renderAt("/admin/access");
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/access/roles"));
    expect(await screen.findByRole("tab", { name: "Roles" })).toHaveAttribute("aria-selected", "true");
    await userEvent.click(screen.getByRole("tab", { name: "Matrix" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/access/matrix"));
    await router.navigate({ to: "/admin/access/$tab", params: { tab: "sso" } });
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/access/sso"));
    expect(await screen.findByLabelText("Issuer")).toBeInTheDocument();
  });
});

describe("the Roles tab", () => {
  it("lists who holds what where, and grants a role in a space", async () => {
    const sent = stubAccess({
      "POST /role-assignments": {
        status: 201,
        body: {
          assignment: {
            id: "g-4",
            role: "proofreader",
            roleName: "Proofreader",
            spaceId: space.id,
            spaceKey: "DOCS",
            spaceName: space.name,
            userId: ids.bob,
            userName: "Bob Builder",
            createdAt: "2026-10-02T08:00:00Z",
          },
        },
      },
    });
    await renderAt("/admin/access/roles");
    const table = await screen.findByRole("table", { name: "Roles" });
    expect(within(table).getByText("Everyone").closest("tr")).toHaveTextContent("Whole installation");
    expect(within(table).getByText("Writers").closest("tr")).toHaveTextContent(`${space.name} (DOCS)`);
    expect(await axeViolations()).toEqual([]);

    const form = document.querySelector<HTMLElement>("[data-grant-form]")!;
    await userEvent.type(within(form).getByRole("combobox", { name: "Person or group to grant the role to" }), "Bo");
    await userEvent.click(await within(form).findByRole("option", { name: /Bob Builder/ }));
    await userEvent.selectOptions(within(form).getByRole("combobox", { name: "Role" }), "administrator");
    expect(within(form).getByRole("combobox", { name: "Where" })).toBeDisabled();
    expect(within(form).getByText(/answers for the whole installation/)).toBeInTheDocument();
    await userEvent.selectOptions(within(form).getByRole("combobox", { name: "Role" }), "proofreader");
    await userEvent.selectOptions(within(form).getByRole("combobox", { name: "Where" }), "DOCS");
    await userEvent.click(within(form).getByRole("button", { name: "Grant" }));
    expect(await screen.findByText("Bob Builder holds Proofreader now.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ role: "proofreader", spaceKey: "DOCS", userId: ids.bob, groupId: null });
  });

  it("asks before a grant goes, and shows the server's refusal in its sentence", async () => {
    const refusal = "Stator keeps at least one active administrator. Make somebody else an administrator first, then take it from this one.";
    stubAccess({ "DELETE /role-assignments/g-1": { status: 409, body: { error: { code: "installation_keeps_administrator", message: refusal } } } });
    await renderAt("/admin/access/roles");
    const table = await screen.findByRole("table", { name: "Roles" });
    await userEvent.click(within(table).getByRole("button", { name: "Revoke Administrator from Ada Lovelace" }));
    await answerConfirm(true);
    expect(await screen.findByText(refusal)).toBeInTheDocument();
  });
});

describe("the Matrix tab", () => {
  it("saves a box the moment it is ticked, and keeps the administrator administering", async () => {
    const sent = stubAccess({ "PATCH /roles/commenter": { status: 200, body: { role: { ...roles[3], permissions: ["view", "addComments", "addPages"] } } } });
    await renderAt("/admin/access/matrix");
    const matrix = await screen.findByRole("table", { name: "Matrix" });
    const fixed = matrix.querySelector<HTMLInputElement>('[data-permission-cell="administrator:administerSite"]')!;
    expect(fixed).toBeChecked();
    expect(fixed).toBeDisabled();
    expect(matrix.querySelector('[data-permission-cell="member:use"]')).toBeChecked();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(matrix.querySelector<HTMLInputElement>('[data-permission-cell="commenter:addPages"]')!);
    expect(await screen.findByText("Commenter may now add, edit and move pages.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PATCH")).toMatchObject({ path: "/roles/commenter", body: { permissions: ["view", "addComments", "addPages"] } });
  });

  it("adds a role, renames one so the page shows the name at once, and deletes one with its grants", async () => {
    let current = roles;
    const sent = stubAccess({
      "GET /roles": () => ({ status: 200, body: { roles: current } }),
      "POST /roles": () => {
        const made = { role: "reviewer", name: "Reviewer", description: "", orgWideOnly: false, builtin: false, permissions: ["view" as const], inUse: 0 };
        current = [...current, made];
        return { status: 201, body: { role: made } };
      },
      "PATCH /roles/editor": () => {
        current = current.map((role) => (role.role === "editor" ? { ...role, name: "Author" } : role));
        return { status: 200, body: { role: current[2] } };
      },
      "DELETE /roles/proofreader": () => {
        current = current.filter((role) => role.role !== "proofreader");
        return { status: 204 };
      },
    });
    await renderAt("/admin/access/matrix");
    await userEvent.click(await screen.findByRole("button", { name: "New role" }));
    const dialog = await screen.findByRole("dialog", { name: "New role" });
    await userEvent.type(within(dialog).getByLabelText("Name"), "Reviewer");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add role" }));
    expect(await screen.findByText("Reviewer was added.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ name: "Reviewer", description: "", orgWideOnly: false, permissions: ["view"] });

    const matrix = screen.getByRole("table", { name: "Matrix" });
    await userEvent.click(within(matrix).getByRole("button", { name: "Actions for Editor" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Rename" }));
    const rename = await screen.findByRole("dialog", { name: "Rename Editor" });
    await userEvent.clear(within(rename).getByLabelText("Name"));
    await userEvent.type(within(rename).getByLabelText("Name"), "Author");
    await userEvent.click(within(rename).getByRole("button", { name: "Save" }));
    expect(await within(matrix).findByText("Author")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PATCH")?.body).toEqual({ name: "Author", description: "" });

    await userEvent.click(within(matrix).getByRole("button", { name: "Actions for Viewer" }));
    expect(await screen.findByRole("menuitem", { name: "Delete" })).toHaveAttribute("aria-disabled", "true");
    await userEvent.keyboard("{Escape}");
    await userEvent.click(within(matrix).getByRole("button", { name: "Actions for Proofreader" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));
    expect(await screen.findByText(/2 grants of Proofreader go with it/)).toBeInTheDocument();
    await answerConfirm(true);
    expect(await screen.findByText("Proofreader was deleted.")).toBeInTheDocument();
  });
});

describe("the Groups tab", () => {
  it("shows Everyone as built in, makes a group the provider fills, and fills one kept by hand", async () => {
    const sent = stubAccess({
      "POST /groups": { status: 201, body: { group: { ...groups[2], id: "g-new", name: "Leads", externalRef: "leads" } } },
      [`POST /groups/${ids.writers}/members`]: { status: 200, body: { group: { ...groups[1], memberCount: 2 } } },
    });
    await renderAt("/admin/access/groups");
    const everyone = await screen.findByText("Every member of the installation, now and later, never a guest. Grant it a role to reach everybody at once.");
    expect(everyone.closest("[data-group-builtin]")).toHaveAttribute("data-group-builtin", "everyone");
    const staff = document.querySelector<HTMLElement>('[data-group="Staff"]')!;
    expect(within(staff).getByText("From the identity provider")).toBeInTheDocument();
    expect(within(staff).queryByRole("combobox")).toBeNull();
    expect(await axeViolations()).toEqual([]);

    await userEvent.type(screen.getByRole("textbox", { name: "New group" }), "Leads");
    await userEvent.type(screen.getByLabelText("Provider claim"), "leads");
    await userEvent.click(screen.getByRole("button", { name: "Make group" }));
    expect(await screen.findByText("Leads was made.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST" && r.path === "/groups")?.body).toEqual({ name: "Leads", externalRef: "leads" });

    const writers = document.querySelector<HTMLElement>('[data-group="Writers"]')!;
    await userEvent.type(within(writers).getByRole("combobox", { name: "Add somebody to Writers" }), "Bo");
    await userEvent.click(await within(writers).findByRole("option", { name: /Bob Builder/ }));
    expect(await screen.findByText("Bob Builder is in Writers now.")).toBeInTheDocument();
    expect(sent.find((r) => r.path === `/groups/${ids.writers}/members`)?.body).toEqual({ userId: ids.bob });
  });
});

describe("the Members tab", () => {
  it("shows each person's standing and the roles they hold, by name or through a group", async () => {
    stubAccess({
      "GET /users": {
        status: 200,
        body: {
          members: [
            {
              userId: ids.ada,
              email: "ada@stator.test",
              name: "Ada Lovelace",
              role: "member",
              active: true,
              joinedAt: "2026-09-01T08:00:00Z",
              guestSpace: null,
              roles: [
                { role: "administrator", name: "Administrator", groupId: null, groupName: null, groupBuiltin: null },
                { role: "member", name: "Member", groupId: ids.everyone, groupName: "Everyone", groupBuiltin: "everyone" },
              ],
            },
            {
              userId: ids.bob,
              email: "bob@stator.test",
              name: "Bob Builder",
              role: "guest",
              active: true,
              joinedAt: "2026-09-02T08:00:00Z",
              guestSpace: { id: space.id, key: "DOCS", name: space.name },
              roles: [],
            },
          ],
        },
      },
    });
    await renderAt("/admin/access/members");
    const ada = (await screen.findByText("ada@stator.test")).closest("tr")!;
    expect(within(ada).getByText("Member", { selector: "[data-member-role]" })).toBeInTheDocument();
    expect(ada.querySelector('[data-member-holds="administrator"]')).toHaveTextContent("Administrator");
    expect(ada.querySelector('[data-member-holds="member"]')).toHaveTextContent("Member through Everyone");
    const bob = screen.getByText("bob@stator.test").closest("tr")!;
    expect(within(bob).getByText("Guest", { selector: "[data-member-role]" })).toBeInTheDocument();
    expect(within(bob).getByText("None")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});
