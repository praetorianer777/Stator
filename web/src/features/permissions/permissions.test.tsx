import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Assignment, RoleDescription } from "@/api/access";
import type { Restrictions } from "@/api/permissions";
import type { TreeNode } from "@/api/tree";
import { answerConfirm, renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
// The edit route's chunk carries the editor and its shared draft. Loaded inside
// a test it outlasted the test's time on a busy machine; loaded here it is at hand.
import "@/routes/page-edit.lazy";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// Under the test's own five seconds.
const EDITOR_CHUNK_WAIT = { timeout: 4_000 };
const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const, administers: false } };
const space = aSpace();
const home = aPage();
const ids = {
  plans: "0195f000-0000-7000-8000-0000000000a1",
  secret: "0195f000-0000-7000-8000-0000000000a2",
  bob: "0195f000-0000-7000-8000-00000000b0b0",
  eng: "0195f000-0000-7000-8000-00000000e000",
};
const people = [{ id: ids.bob, name: "Bob Builder", email: "bob@stator.test" }];
const everyoneId = "0195f000-0000-7000-8000-00000000e0e0";
const groups = [
  {
    id: everyoneId,
    name: "Everyone",
    description: "",
    source: "local",
    externalRef: "",
    builtin: "everyone",
    memberCount: 9,
    roleCount: 2,
    fromProvider: false,
  },
  { id: ids.eng, name: "Engineering", description: "", source: "oidc", externalRef: "engineering", memberCount: 4, roleCount: 0, fromProvider: true },
];
const pickers = {
  "GET /people": { status: 200, body: { people } },
  "GET /groups": { status: 200, body: { groups } },
};
const everyone = { type: "group" as const, id: everyoneId, name: "Everyone" };
const ada = { type: "user" as const, id: signedIn.user.id, name: "Ada Lovelace" };
const bob = { type: "user" as const, id: ids.bob, name: "Bob Builder" };
const eng = { type: "group" as const, id: ids.eng, name: "Engineering" };
const conflict: Answer = { status: 409, body: { error: { code: "conflict", message: "The change would lock you out." } } };
const noAccess: Answer = { status: 403, body: { error: { code: "no_access", message: "You may not use this organization." } } };

async function pick(label: string, typed: string, option: string) {
  const box = screen.getAllByRole("combobox", { name: label })[0]!;
  await userEvent.type(box, typed);
  await userEvent.click(await screen.findByRole("option", { name: new RegExp(option) }));
}

describe("the installation's access", () => {
  it("is not offered to a member, and the address of before leads to the Access page", async () => {
    stubApi({ "GET /access/me": { status: 200, body: { can: { use: true, createSpace: false, administer: false } } } });
    const router = await renderAt("/settings/permissions", { me: member });
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/access/roles"));
    expect(await screen.findByText("Only administrators can open this page")).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Admin sections" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Admin" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    expect(screen.queryByRole("menuitem", { name: "Access" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Admin" })).toBeNull();
  });

  it("let a member who may create spaces see the way to", async () => {
    stubApi({
      "GET /access/me": { status: 200, body: { can: { use: true, createSpace: true, administer: false } } },
      "GET /spaces": { status: 200, body: { spaces: [] } },
    });
    await renderAt("/spaces", { me: member });
    expect(await screen.findAllByRole("button", { name: "Create space" })).not.toHaveLength(0);
  });
});

describe("a member the organization does not let in", () => {
  it("sees why and what to do instead of the application", async () => {
    stubApi({ "GET /spaces": noAccess, "GET /access/me": { status: 200, body: { can: { use: true, createSpace: false, administer: false } } } });
    await renderAt("/spaces", { me: member });
    expect(await screen.findByRole("heading", { name: "You do not have access to Demo" })).toBeInTheDocument();
    expect(screen.getByText(/Ask an administrator to grant you one under Access/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign out" })).toBeInTheDocument();
    expect(document.querySelector("[data-top-bar]")).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("is told so straight away when the organization says it", async () => {
    stubApi({ "GET /access/me": { status: 200, body: { can: { use: false, createSpace: false, administer: false } } } });
    await renderAt("/", { me: member });
    expect(await screen.findByRole("heading", { name: "You do not have access to Demo" })).toBeInTheDocument();
  });
});

const roles: RoleDescription[] = [
  {
    role: "administrator",
    name: "Administrator",
    description: "",
    orgWideOnly: true,
    builtin: true,
    permissions: ["view", "addComments", "addPages", "delete", "administer", "use", "createSpace", "administerSite"],
    inUse: 1,
  },
  {
    role: "space_administrator",
    name: "Space administrator",
    description: "",
    orgWideOnly: false,
    builtin: true,
    permissions: ["view", "addComments", "addPages", "delete", "administer"],
    inUse: 1,
  },
  { role: "editor", name: "Editor", description: "", orgWideOnly: false, builtin: true, permissions: ["view", "addComments", "addPages", "delete"], inUse: 1 },
  { role: "commenter", name: "Commenter", description: "", orgWideOnly: false, builtin: true, permissions: ["view", "addComments"], inUse: 0 },
  { role: "viewer", name: "Viewer", description: "", orgWideOnly: false, builtin: true, permissions: ["view"], inUse: 0 },
];
const grantOf = (role: string, roleName: string, who: { id: string; name: string; group?: boolean }, id: string): Assignment => ({
  id,
  role,
  roleName,
  spaceId: space.id,
  spaceKey: "DOCS",
  spaceName: space.name,
  createdAt: "2026-10-01T08:00:00Z",
  ...(who.group
    ? { groupId: who.id, groupName: who.name, ...(who.id === everyoneId ? { groupBuiltin: "everyone" as const } : {}) }
    : { userId: who.id, userName: who.name }),
});
const table: Assignment[] = [
  grantOf("space_administrator", "Space administrator", { id: ada.id!, name: ada.name }, "0195f000-0000-7000-8000-0000000000c1"),
  grantOf("editor", "Editor", { id: everyoneId, name: "Everyone", group: true }, "0195f000-0000-7000-8000-0000000000c2"),
];
const page = (assignments: Assignment[]) => ({ assignments, total: assignments.length, limit: 50, offset: 0 });

describe("a space's permissions", () => {
  it("grant roles to people and groups and take them away", async () => {
    let grants = table;
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /roles": { status: 200, body: { roles } },
      "GET /role-assignments": () => ({ status: 200, body: page(grants) }),
      "POST /role-assignments": () => {
        const made = grantOf("commenter", "Commenter", { id: ids.bob, name: "Bob Builder" }, "0195f000-0000-7000-8000-0000000000c3");
        grants = [...grants, made];
        return { status: 201, body: { assignment: made } };
      },
      "DELETE /role-assignments/0195f000-0000-7000-8000-0000000000c2": () => {
        grants = grants.filter((g) => g.id !== "0195f000-0000-7000-8000-0000000000c2");
        return { status: 204 };
      },
      ...pickers,
    });
    await renderAt("/spaces/DOCS/settings/members");
    const list = await screen.findByRole("table", { name: "Roles granted in this space" });
    expect(within(list).getByText("Everyone").closest("tr")).toHaveAttribute("data-grant-role", "editor");
    expect(within(list).getByText("Ada Lovelace").closest("tr")).toHaveAttribute("data-grant-role", "space_administrator");
    const words = document.querySelector("[data-space-role=commenter]")!;
    expect(words).toHaveTextContent("Commenter");
    expect(words).toHaveTextContent("View the space and its pages, Comment on pages");
    expect(document.querySelector("[data-space-role=administrator]")).toBeNull();
    expect(await axeViolations()).toEqual([]);

    await pick("Grant a role to a person or group", "Bo", "Bob Builder");
    const form = document.querySelector<HTMLElement>("[data-grant-form]")!;
    expect(within(form).queryByRole("option", { name: "Administrator" })).toBeNull();
    await userEvent.selectOptions(within(form).getByRole("combobox", { name: "Role" }), "commenter");
    await userEvent.click(within(form).getByRole("button", { name: "Grant" }));
    expect(await screen.findByText("Bob Builder holds Commenter here now.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ role: "commenter", spaceKey: "DOCS", userId: ids.bob, groupId: null });
    expect(await within(list).findByText("Bob Builder")).toBeInTheDocument();

    await userEvent.click(within(list).getByRole("button", { name: "Revoke Editor from Everyone" }));
    await answerConfirm(true);
    expect(await screen.findByText("Everyone no longer holds Editor here.")).toBeInTheDocument();
    expect(sent.some((r) => r.method === "DELETE" && r.path === "/role-assignments/0195f000-0000-7000-8000-0000000000c2")).toBe(true);
    await waitFor(() => expect(within(list).queryByText("Everyone")).toBeNull());
  });

  it("are hidden from somebody who does not administer the space, and opened by address say who to ask", async () => {
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space: aSpace({ can: { ...space.can, administer: false, delete: false, purgeTrash: false } }) } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    });
    await renderAt("/spaces/DOCS/settings/members", { me: member });
    expect(await screen.findByText("Only the space's administrators change its settings")).toBeInTheDocument();
    expect(screen.getByText("Ask one of them, or ask for the role.")).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Members and permissions" })).toBeNull();
    expect(sent.some((r) => r.path === "/role-assignments")).toBe(false);
  });
});

describe("copying another space's permissions", () => {
  const ops = aSpace({ id: "0195f000-0000-7000-8000-0000000000f5", key: "OPS", name: "Operations" });
  const mine = aSpace({ id: "0195f000-0000-7000-8000-0000000000f6", key: "ADA", name: "Ada's notes", owner: { id: ada.id!, name: ada.name } });
  const closed = aSpace({ id: "0195f000-0000-7000-8000-0000000000f7", key: "HR", name: "People", can: { ...space.can, administer: false } });
  const subject = (s: typeof everyone | typeof bob | typeof eng, guest = false) => ({ ...s, guest });
  const preview = (mode: "merge" | "replace", over: Record<string, unknown> = {}) => ({
    source: { key: "OPS", name: "Operations" },
    target: { key: "DOCS", name: "Handbook" },
    mode,
    changes:
      mode === "merge"
        ? [{ subject: subject(eng), kind: "added", before: [], after: ["view", "addPages"] }]
        : [
            { subject: subject(everyone), kind: "narrowed", before: ["view", "addPages", "addComments", "delete"], after: ["view"] },
            { subject: subject(eng), kind: "added", before: [], after: ["view", "addPages"] },
            { subject: { type: "anonymous", id: null, name: "Anybody", guest: false }, kind: "added", before: [], after: ["view"] },
          ],
    skipped: [{ subject: { type: "user", id: ids.bob, name: "Gwen Guest", guest: true }, permissions: ["view"], reason: "guest" }],
    kept: [],
    counts: { added: 1, widened: 0, narrowed: 0, changed: 0, removed: 0, skipped: 1, unchanged: 2 },
    leavesNoAdministrator: false,
    fingerprint: `print-${mode}`,
    ...over,
  });
  const base = {
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    "GET /spaces": { status: 200, body: { spaces: [space, ops, mine, closed] } },
    "GET /roles": { status: 200, body: { roles } },
    ...pickers,
  };

  it("previews the changes and applies exactly the preview shown", async () => {
    const copied: Assignment[] = [
      ...table,
      grantOf("editor", "Editor", { id: ids.eng, name: "Engineering", group: true }, "0195f000-0000-7000-8000-0000000000c4"),
    ];
    let saved = table;
    const sent = stubApi({
      ...base,
      "GET /role-assignments": () => ({ status: 200, body: page(saved) }),
      "GET /spaces/DOCS/permissions/copy": (request) => {
        const mode = new URL(request.url, "http://app.test").searchParams.get("mode") as "merge" | "replace";
        return { status: 200, body: { preview: preview(mode) } };
      },
      "GET /spaces/DOCS/anonymous-access": { status: 200, body: { anonymousAccess: { view: false, orgEnabled: false, personal: false } } },
      "POST /spaces/DOCS/permissions/copy": () => {
        saved = copied;
        return { status: 200, body: { grants: copied, copy: preview("merge") } };
      },
    });
    await renderAt("/spaces/DOCS/settings/members");
    await userEvent.click(await screen.findByRole("button", { name: "Copy from another space" }));
    const dialog = await screen.findByRole("dialog", { name: "Copy permissions into Handbook" });
    const source = within(dialog).getByRole("combobox", { name: "Copy from" });
    expect(
      within(source)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Choose a space", "Operations (OPS)"]);
    expect(within(dialog).getByRole("button", { name: "Copy permissions" })).toBeDisabled();

    await userEvent.selectOptions(source, "OPS");
    const diff = await within(dialog).findByRole("table", { name: "Changes to this space's permissions" });
    expect(within(diff).getByText("Engineering").closest("tr")).toHaveAttribute("data-copy-kind", "added");
    expect(within(dialog).getByText(/Gwen Guest is a guest of the other space/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(dialog).getByRole("button", { name: "Replace" }));
    expect(await within(dialog).findByText("Anybody without signing in")).toBeInTheDocument();
    const replaced = within(dialog).getByRole("table", { name: "Changes to this space's permissions" });
    expect(within(replaced).getByText("Everyone").closest("tr")).toHaveAttribute("data-copy-kind", "narrowed");
    await userEvent.click(within(dialog).getByRole("button", { name: "Merge" }));
    await waitFor(() => expect(within(dialog).queryByText("Anybody without signing in")).toBeNull());

    await userEvent.click(within(dialog).getByRole("button", { name: "Copy permissions" }));
    expect(await screen.findByText("Copied the permissions of Operations.")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ from: "OPS", mode: "merge", fingerprint: "print-merge" });
    const grid = screen.getByRole("table", { name: "Roles granted in this space" });
    expect(await within(grid).findByText("Engineering")).toBeInTheDocument();
  });

  it("refuses a copy that leaves nobody administering, and shows a stale preview afresh", async () => {
    let looks = 0;
    stubApi({
      ...base,
      "GET /role-assignments": { status: 200, body: page(table) },
      "GET /spaces/DOCS/permissions/copy": (request) => {
        looks += 1;
        const mode = new URL(request.url, "http://app.test").searchParams.get("mode");
        return {
          status: 200,
          body: { preview: mode === "replace" ? preview("replace", { leavesNoAdministrator: true }) : preview("merge", { fingerprint: `print-${looks}` }) },
        };
      },
      "POST /spaces/DOCS/permissions/copy": {
        status: 409,
        body: {
          error: {
            code: "copy_changed",
            message: "The permissions of OPS or DOCS changed since the preview was made. Look at the new preview, then copy again.",
          },
        },
      },
    });
    await renderAt("/spaces/DOCS/settings/members");
    await userEvent.click(await screen.findByRole("button", { name: "Copy from another space" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.selectOptions(within(dialog).getByRole("combobox", { name: "Copy from" }), "OPS");
    await userEvent.click(within(dialog).getByRole("button", { name: "Replace" }));
    expect(await within(dialog).findByText(/would leave the space without an administrator/)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Copy permissions" })).toBeDisabled();

    await userEvent.click(within(dialog).getByRole("button", { name: "Merge" }));
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "Copy permissions" })).toBeEnabled());
    const before = looks;
    await userEvent.click(within(dialog).getByRole("button", { name: "Copy permissions" }));
    expect(await within(dialog).findByText(/changed since the preview was made/)).toBeInTheDocument();
    await waitFor(() => expect(looks).toBeGreaterThan(before));
  });
});

describe("a page's restrictions", () => {
  const plans = { id: ids.plans, title: "Plans", home: false };
  const secret = aPage({
    id: ids.secret,
    title: "Secret",
    home: false,
    parentId: ids.plans,
    restricted: { view: true, edit: false },
    ancestors: [{ id: home.id, title: "Handbook", home: true }, plans],
  });
  const inherited: Restrictions = { view: [], edit: [], editGrant: [], inherited: [{ page: plans, view: [eng], edit: [], editGrant: [] }] };
  const tree: Record<string, TreeNode[]> = {
    home: [
      { id: ids.plans, parentId: home.id, title: "Plans", hasChildren: true, unpublished: false, restricted: true, archived: false, kind: "page", icon: null },
    ],
    [ids.plans]: [
      {
        id: ids.secret,
        parentId: ids.plans,
        title: "Secret",
        hasChildren: false,
        unpublished: false,
        restricted: true,
        archived: false,
        kind: "page",
        icon: null,
      },
    ],
  };
  const stubPage = (page = secret, more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) =>
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": (request) => ({ status: 200, body: { pages: tree[new URL(request.url).searchParams.get("parent") ?? "home"] ?? [] } }),
      [`GET /pages/${page.id}`]: { status: 200, body: { page, space } },
      [`GET /pages/${page.id}/restrictions`]: { status: 200, body: { restrictions: inherited } },
      ...pickers,
      ...more,
    });

  it("mark a restricted page on the page and in the tree", async () => {
    stubPage();
    await renderAt(`/spaces/DOCS/p/${ids.secret}/secret`);
    expect(await screen.findByRole("button", { name: /^Restricted/ })).toHaveAttribute("data-page-restricted", "view");
    const row = await screen.findByRole("treeitem", { name: "Secret" });
    expect(within(row).getByText("restricted")).toBeInTheDocument();
    expect(within(screen.getByRole("treeitem", { name: "Plans" })).getAllByText("restricted")).not.toHaveLength(0);
  });

  it("show what is inherited and from where, and save the page's own lists", async () => {
    const sent = stubPage(secret, { [`PUT /pages/${ids.secret}/restrictions`]: { status: 200, body: { restrictions: { ...inherited, view: [bob, ada] } } } });
    await renderAt(`/spaces/DOCS/p/${ids.secret}/secret`);
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Restrictions" }));
    const dialog = await screen.findByRole("dialog", { name: "Restrictions on Secret" });
    const from = await within(dialog).findByRole("link", { name: "From Plans" });
    expect(from.closest("li")).toHaveTextContent("Engineering");
    expect(within(dialog).getByText("Everyone who can view the space.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    const viewList = within(dialog).getByRole("region", { name: "Who can view" });
    await userEvent.type(within(viewList).getByRole("combobox"), "Bo");
    await userEvent.click(await within(viewList).findByRole("option", { name: /Bob Builder/ }));
    await userEvent.click(within(viewList).getByRole("button", { name: "Add yourself" }));
    expect(within(viewList).getByText(/Ada Lovelace/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Save restrictions" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({
      view: [
        { type: "user", id: ids.bob },
        { type: "user", id: ada.id },
      ],
      edit: [],
    });
  });

  it("say what to do when a save would lock the caller out", async () => {
    stubPage(secret, { [`PUT /pages/${ids.secret}/restrictions`]: conflict });
    await renderAt(`/spaces/DOCS/p/${ids.secret}/secret`);
    await userEvent.click(await screen.findByRole("button", { name: /^Restricted/ }));
    const dialog = await screen.findByRole("dialog", { name: "Restrictions on Secret" });
    const editList = within(dialog).getByRole("region", { name: "Who can edit" });
    await userEvent.type(within(editList).getByRole("combobox"), "Bo");
    await userEvent.click(await within(editList).findByRole("option", { name: /Bob Builder/ }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save restrictions" }));
    expect(await within(dialog).findByText(/Saving this would shut you out of the page\. Add yourself/)).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("offer the home page an edit list but no view list", async () => {
    stubPage(home, { [`GET /pages/${home.id}/restrictions`]: { status: 200, body: { restrictions: { view: [], edit: [], editGrant: [], inherited: [] } } } });
    await renderAt("/spaces/DOCS");
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Restrictions" }));
    const dialog = await screen.findByRole("dialog");
    const viewList = within(dialog).getByRole("region", { name: "Who can view" });
    expect(within(viewList).getByText(/The home page is seen by everyone who can view the space/)).toBeInTheDocument();
    expect(within(viewList).queryByRole("combobox")).toBeNull();
    expect(within(within(dialog).getByRole("region", { name: "Who can edit" })).getByRole("combobox")).toBeInTheDocument();
    expect(within(dialog).getByText("No page above this one is restricted.")).toBeInTheDocument();
  });

  it("let an administrator of the space name who else may edit, and mark who on the edit list cannot", async () => {
    const admin = aPage({ ...secret, can: { ...secret.can, grantEdit: true } });
    const sent = stubPage(admin, {
      [`POST /pages/${ids.secret}/restrictions/check`]: async (request) => {
        const body = (await request.json()) as { edit: { id: string }[]; editGrant: { id: string }[] };
        const blocked = body.edit.some((each) => each.id === ids.bob) && !body.editGrant.some((each) => each.id === ids.bob);
        return { status: 200, body: { check: { cannotEdit: blocked ? [{ subject: bob, members: 0 }] : [] } } };
      },
      [`PUT /pages/${ids.secret}/restrictions`]: { status: 200, body: { restrictions: { ...inherited, edit: [bob], editGrant: [bob] } } },
    });
    await renderAt(`/spaces/DOCS/p/${ids.secret}/secret`);
    await userEvent.click(await screen.findByRole("button", { name: /^Restricted/ }));
    const dialog = await screen.findByRole("dialog", { name: "Restrictions on Secret" });
    const editList = within(dialog).getByRole("region", { name: "Who can edit" });
    await userEvent.type(within(editList).getByRole("combobox"), "Bo");
    await userEvent.click(await within(editList).findByRole("option", { name: /Bob Builder/ }));
    expect(await within(editList).findByText("Cannot edit")).toBeInTheDocument();
    expect(within(editList).getByText(/because the space does not let them add pages\. Add them to Also allowed to edit/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    const grantList = within(dialog).getByRole("region", { name: "Also allowed to edit" });
    await userEvent.type(within(grantList).getByRole("combobox"), "Bo");
    await userEvent.click(await within(grantList).findByRole("option", { name: /Bob Builder/ }));
    await waitFor(() => expect(within(editList).queryByText("Cannot edit")).toBeNull());
    await userEvent.click(within(dialog).getByRole("button", { name: "Save restrictions" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ view: [], edit: [{ type: "user", id: ids.bob }], editGrant: [{ type: "user", id: ids.bob }] });
  });

  it("show who else may edit, without changing it, to an editor who does not administer the space", async () => {
    stubPage(secret, {
      [`GET /pages/${ids.secret}/restrictions`]: {
        status: 200,
        body: { restrictions: { ...inherited, editGrant: [bob], inherited: [{ page: plans, view: [], edit: [], editGrant: [eng] }] } },
      },
      [`POST /pages/${ids.secret}/restrictions/check`]: { status: 200, body: { check: { cannotEdit: [{ subject: eng, members: 2 }] } } },
    });
    await renderAt(`/spaces/DOCS/p/${ids.secret}/secret`, { me: member });
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Restrictions" }));
    const dialog = await screen.findByRole("dialog", { name: "Restrictions on Secret" });
    const grantList = within(dialog).getByRole("region", { name: "Also allowed to edit" });
    expect(within(grantList).getByText("Bob Builder")).toBeInTheDocument();
    expect(within(grantList).queryByRole("combobox")).toBeNull();
    expect(within(grantList).getByText("Only administrators of the space change this list.")).toBeInTheDocument();
    expect(within(dialog).getByRole("link", { name: "From Plans" }).closest("li")).toHaveTextContent("Also edit");

    const editList = within(dialog).getByRole("region", { name: "Who can edit" });
    await userEvent.type(within(editList).getByRole("combobox"), "Eng");
    await userEvent.click(await within(editList).findByRole("option", { name: /Engineering/ }));
    expect(await within(editList).findByText("2 members cannot edit")).toBeInTheDocument();
    expect(within(editList).getByText(/Ask an administrator of the space to add them to Also allowed to edit/)).toBeInTheDocument();
  });

  it("are shown but not changeable to a reader who cannot edit the page", async () => {
    const readOnly = aPage({ ...secret, can: { edit: false, delete: false, restrict: false, comment: true, archive: false, add: false, grantEdit: false } });
    stubPage(readOnly);
    await renderAt(`/spaces/DOCS/p/${ids.secret}/secret`, { me: member });
    await screen.findByRole("button", { name: /^Restricted/ });
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "New page" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: /^Restricted/ }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Only people who can edit this page can change its restrictions.")).toBeInTheDocument();
    expect(within(dialog).queryByRole("combobox")).toBeNull();
    expect(within(dialog).queryByRole("button", { name: "Save restrictions" })).toBeNull();
  });
});

describe("what a page offers", () => {
  const page = aPage({ id: ids.plans, title: "Plans", home: false, parentId: home.id, ancestors: [{ id: home.id, title: "Handbook", home: true }] });
  const stub = (can: typeof page.can) =>
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [space] } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      [`GET /pages/${ids.plans}`]: { status: 200, body: { page: { ...page, can }, space } },
    });

  it("follows page.can rather than the space", async () => {
    stub({ edit: true, delete: false, restrict: true, comment: true, archive: false, add: true, grantEdit: false });
    await renderAt(`/spaces/DOCS/p/${ids.plans}/plans`);
    expect(await screen.findByRole("button", { name: "Edit" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Page actions" }));
    expect(await screen.findByRole("menuitem", { name: "Move" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Move to trash" })).toBeNull();
  });

  it("sends somebody who may not edit away from the editor with a sentence", async () => {
    stub({ edit: false, delete: false, restrict: false, comment: false, archive: false, add: false, grantEdit: false });
    await renderAt(`/spaces/DOCS/p/${ids.plans}/plans/edit`);
    expect(await screen.findByText(/You can read this page but not edit it\./, undefined, EDITOR_CHUNK_WAIT)).toBeInTheDocument();
  });
});
