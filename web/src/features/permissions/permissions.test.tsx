import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { GlobalGrant, Restrictions, SpaceGrant } from "@/api/permissions";
import type { TreeNode } from "@/api/tree";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { impliedPermissions } from "./SpacePermissions";
// The edit route's chunk carries the editor and its shared draft. Loaded inside
// a test it outlasted the test's time on a busy machine; loaded here it is at hand.
import "@/routes/page-edit.lazy";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// Under the test's own five seconds.
const EDITOR_CHUNK_WAIT = { timeout: 4_000 };
const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const } };
const space = aSpace();
const home = aPage();
const ids = {
  plans: "0195f000-0000-7000-8000-0000000000a1",
  secret: "0195f000-0000-7000-8000-0000000000a2",
  bob: "0195f000-0000-7000-8000-00000000b0b0",
  eng: "0195f000-0000-7000-8000-00000000e000",
};
const people = [{ id: ids.bob, name: "Bob Builder", email: "bob@stator.test" }];
const groups = [{ id: ids.eng, name: "Engineering", memberCount: 4, fromProvider: true }];
const pickers = {
  "GET /people": { status: 200, body: { people } },
  "GET /groups": { status: 200, body: { groups } },
};
const everyone = { type: "everyone" as const, id: null, name: "Everyone" };
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

describe("the organization's permissions", () => {
  const grants: GlobalGrant[] = [
    { permission: "use", fixed: false, subjects: [everyone] },
    { permission: "createSpace", fixed: false, subjects: [] },
    { permission: "administer", fixed: true, subjects: [ada] },
  ];

  it("let an administrator grant creating spaces to a group, saved only on Save", async () => {
    const sent = stubApi({
      "GET /access/me": { status: 200, body: { can: { use: true, createSpace: true, administer: true } } },
      "GET /org/permissions": { status: 200, body: { permissions: grants } },
      "PUT /org/permissions/createSpace": { status: 200, body: { permission: { ...grants[1], subjects: [eng] } } },
      ...pickers,
    });
    await renderAt("/");
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Permissions" }));
    const card = (await screen.findByText("Create spaces")).closest<HTMLElement>("[data-global-permission]")!;
    expect(within(card).getByText("Nobody but the administrators.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.type(within(card).getByRole("combobox", { name: "Add a person or group" }), "Eng");
    await userEvent.click(await within(card).findByRole("option", { name: /Engineering/ }));
    expect(within(card).getByText("Added Engineering.")).toBeInTheDocument();
    expect(within(card).getByText("Engineering", { selector: "[data-subject] span" })).toBeInTheDocument();
    expect(sent.some((r) => r.method === "PUT")).toBe(false);
    await userEvent.click(within(card).getByRole("button", { name: "Save changes" }));
    expect(await within(card).findByText("Saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")).toMatchObject({ path: "/org/permissions/createSpace", body: { subjects: [{ type: "group", id: ids.eng }] } });
  });

  it("show administering as following the roles, with no way to change it here", async () => {
    stubApi({ "GET /org/permissions": { status: 200, body: { permissions: grants } }, ...pickers });
    await renderAt("/settings/permissions");
    const card = (await screen.findByText("Administer the organization")).closest<HTMLElement>("[data-global-permission]")!;
    expect(within(card).getByText(/follows the roles of the organization's members/)).toBeInTheDocument();
    expect(within(card).queryByRole("combobox")).toBeNull();
    expect(within(card).queryByRole("button", { name: /Remove/ })).toBeNull();
    const use = screen.getByText("Use Stator").closest<HTMLElement>("[data-global-permission]")!;
    expect(within(use).getByRole("button", { name: "Remove Everyone" })).toBeInTheDocument();
  });

  it("are not offered to a member", async () => {
    stubApi({ "GET /access/me": { status: 200, body: { can: { use: true, createSpace: false, administer: false } } } });
    await renderAt("/settings/permissions", { me: member });
    expect(await screen.findByText("Only an administrator of the organization can see and change its permissions.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    expect(screen.queryByRole("menuitem", { name: "Permissions" })).toBeNull();
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
    expect(screen.getByText(/Ask one of its administrators to add you under Permissions/)).toBeInTheDocument();
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

describe("the impliedPermissions of a row", () => {
  it("adds viewing to anything, and everything to administering", () => {
    expect(impliedPermissions(["addPages"])).toEqual(["view", "addPages"]);
    expect(impliedPermissions(["administer"])).toEqual(["view", "addPages", "addComments", "delete", "administer"]);
    expect(impliedPermissions([])).toEqual([]);
  });
});

describe("a space's permissions", () => {
  const table: SpaceGrant[] = [
    { subject: everyone, permissions: ["view", "addPages", "addComments", "delete"] },
    { subject: ada, permissions: ["view", "addPages", "addComments", "delete", "administer"] },
  ];

  it("are a grid an administrator changes and then saves", async () => {
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/permissions": { status: 200, body: { grants: table } },
      "PUT /spaces/DOCS/permissions": { status: 200, body: { grants: table } },
      ...pickers,
    });
    await renderAt("/s/DOCS/settings?tab=permissions");
    const grid = await screen.findByRole("table", { name: "Permissions in this space" });
    const everyoneRow = within(grid).getByText("Everyone").closest("tr")!;
    expect(within(everyoneRow).getByRole("checkbox", { name: "Everyone may view" })).toBeDisabled();
    expect(within(everyoneRow).getByRole("checkbox", { name: "Everyone may administer" })).not.toBeChecked();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(everyoneRow).getByRole("checkbox", { name: "Everyone may add pages" }));
    await userEvent.click(within(everyoneRow).getByRole("checkbox", { name: "Everyone may delete" }));
    await pick("Add a person or group", "Bo", "Bob Builder");
    const bobRow = within(grid).getByText("Bob Builder").closest("tr")!;
    expect(within(bobRow).getByRole("checkbox", { name: "Bob Builder may view" })).toBeChecked();
    await userEvent.click(within(bobRow).getByRole("checkbox", { name: "Bob Builder may administer" }));
    expect(within(bobRow).getByRole("checkbox", { name: "Bob Builder may add pages" })).toBeChecked();
    expect(within(bobRow).getByRole("checkbox", { name: "Bob Builder may add pages" })).toBeDisabled();
    expect(sent.some((r) => r.method === "PUT")).toBe(false);

    await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({
      grants: [
        { subject: { type: "everyone" }, permissions: ["view", "addComments"] },
        { subject: { type: "user", id: ada.id }, permissions: ["view", "addPages", "addComments", "delete", "administer"] },
        { subject: { type: "user", id: ids.bob }, permissions: ["view", "addPages", "addComments", "delete", "administer"] },
      ],
    });
  });

  it("drop a row that is removed, and discard puts everything back", async () => {
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
      "GET /spaces/DOCS/permissions": { status: 200, body: { grants: table } },
      "PUT /spaces/DOCS/permissions": { status: 200, body: { grants: [table[1]] } },
    });
    await renderAt("/s/DOCS/settings?tab=permissions");
    await userEvent.click(await screen.findByRole("button", { name: "Remove Everyone" }));
    expect(screen.queryByRole("checkbox", { name: "Everyone may view" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Discard changes" }));
    expect(screen.getByRole("checkbox", { name: "Everyone may view" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Remove Everyone" }));
    await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ grants: [{ subject: { type: "user", id: ada.id }, permissions: table[1]!.permissions }] }),
    );
  });

  it("are not shown to somebody who does not administer the space", async () => {
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space: aSpace({ can: { ...space.can, administer: false, delete: false, purgeTrash: false } }) } },
      "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    });
    await renderAt("/s/DOCS/settings?tab=permissions", { me: member });
    expect(await screen.findByText("Only an administrator of this space can see and change who may do what in it.")).toBeInTheDocument();
    expect(sent.some((r) => r.path === "/spaces/DOCS/permissions")).toBe(false);
  });
});

describe("copying another space's permissions", () => {
  const table: SpaceGrant[] = [
    { subject: everyone, permissions: ["view", "addPages", "addComments", "delete"] },
    { subject: ada, permissions: ["view", "addPages", "addComments", "delete", "administer"] },
  ];
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
    ...pickers,
  };

  it("previews the changes and applies exactly the preview shown", async () => {
    const copied: SpaceGrant[] = [...table, { subject: eng, permissions: ["view", "addPages"] }];
    let saved = table;
    const sent = stubApi({
      ...base,
      "GET /spaces/DOCS/permissions": () => ({ status: 200, body: { grants: saved } }),
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
    await renderAt("/s/DOCS/settings?tab=permissions");
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
    const grid = screen.getByRole("table", { name: "Permissions in this space" });
    expect(within(grid).getByText("Engineering")).toBeInTheDocument();
  });

  it("refuses a copy that leaves nobody administering, and shows a stale preview afresh", async () => {
    let looks = 0;
    stubApi({
      ...base,
      "GET /spaces/DOCS/permissions": { status: 200, body: { grants: table } },
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
    await renderAt("/s/DOCS/settings?tab=permissions");
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

  it("is offered only once the grid's own changes are saved", async () => {
    stubApi({ ...base, "GET /spaces/DOCS/permissions": { status: 200, body: { grants: table } } });
    await renderAt("/s/DOCS/settings?tab=permissions");
    const offer = await screen.findByRole("button", { name: "Copy from another space" });
    await userEvent.click(screen.getByRole("button", { name: "Remove Everyone" }));
    expect(offer).toBeDisabled();
    expect(screen.getByText("Save or discard your changes above before you copy.")).toBeInTheDocument();
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
    await renderAt(`/s/DOCS/p/${ids.secret}/secret`);
    expect(await screen.findByRole("button", { name: /^Restricted/ })).toHaveAttribute("data-page-restricted", "view");
    const row = await screen.findByRole("treeitem", { name: "Secret" });
    expect(within(row).getByText("restricted")).toBeInTheDocument();
    expect(within(screen.getByRole("treeitem", { name: "Plans" })).getAllByText("restricted")).not.toHaveLength(0);
  });

  it("show what is inherited and from where, and save the page's own lists", async () => {
    const sent = stubPage(secret, { [`PUT /pages/${ids.secret}/restrictions`]: { status: 200, body: { restrictions: { ...inherited, view: [bob, ada] } } } });
    await renderAt(`/s/DOCS/p/${ids.secret}/secret`);
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
    await renderAt(`/s/DOCS/p/${ids.secret}/secret`);
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
    await renderAt("/s/DOCS");
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
    await renderAt(`/s/DOCS/p/${ids.secret}/secret`);
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
    await renderAt(`/s/DOCS/p/${ids.secret}/secret`, { me: member });
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
    await renderAt(`/s/DOCS/p/${ids.secret}/secret`, { me: member });
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
    await renderAt(`/s/DOCS/p/${ids.plans}/plans`);
    expect(await screen.findByRole("button", { name: "Edit" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Page actions" }));
    expect(await screen.findByRole("menuitem", { name: "Move" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Move to trash" })).toBeNull();
  });

  it("sends somebody who may not edit away from the editor with a sentence", async () => {
    stub({ edit: false, delete: false, restrict: false, comment: false, archive: false, add: false, grantEdit: false });
    await renderAt(`/s/DOCS/p/${ids.plans}/plans/edit`);
    expect(await screen.findByText(/You can read this page but not edit it\./, undefined, EDITOR_CHUNK_WAIT)).toBeInTheDocument();
  });
});
