import { describe, expect, it } from "vitest";
import { crumbsFor, pageOfPath, spaceOfPath } from "./Crumbs";

const page = { id: "p-2", title: "Runbook", home: false, kind: "page" as const, ancestors: [{ id: "p-home", title: "Handbook", home: true }] };
const trail = (pathname: string, names = {}) => crumbsFor(pathname, names).map(({ label, to }) => [label, to]);

describe("the trail above a page", () => {
  it("is empty on the places of the wiki, the settings hub and the Admin page", () => {
    for (const path of ["/", "/home", "/search", "/reviews", "/tasks", "/spaces", "/notifications", "/settings", "/admin"]) expect(trail(path)).toEqual([]);
  });

  it("leads back to Spaces from a space's own pages", () => {
    expect(trail("/spaces/new")).toEqual([["Spaces", "/spaces"]]);
    expect(trail("/spaces/DOCS")).toEqual([["Spaces", "/spaces"]]);
    expect(trail("/spaces/DOCS/blog", { spaceName: "Docs" })).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/spaces/DOCS"],
    ]);
  });

  it("names the space by its key until the space is known", () => {
    expect(trail("/spaces/DOCS/settings/general")).toEqual([
      ["Spaces", "/spaces"],
      ["DOCS", "/spaces/DOCS"],
    ]);
  });

  it("passes every page above a page, the home page by its space's name", () => {
    const names = { spaceName: "Docs", page };
    expect(trail("/spaces/DOCS/p/p-2/runbook", names)).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/spaces/DOCS"],
    ]);
    expect(trail("/spaces/DOCS/p/p-2/runbook/edit", names)).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/spaces/DOCS"],
    ]);
  });

  it("puts the page itself, and its history below it, above a version", () => {
    const names = { spaceName: "Docs", page };
    expect(trail("/spaces/DOCS/p/p-2/runbook/history", names)).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/spaces/DOCS"],
      ["Runbook", "/spaces/DOCS/p/p-2/runbook"],
    ]);
  });

  it("puts a post in its space's blog", () => {
    const post = { ...page, kind: "post" as const, ancestors: [] };
    expect(trail("/spaces/DOCS/p/p-2/runbook", { spaceName: "Docs", page: post })).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/spaces/DOCS"],
      ["Blog", "/spaces/DOCS/blog"],
    ]);
  });

  it("leads back to Settings, and to the settings page a deeper one belongs to", () => {
    expect(trail("/settings/tokens")).toEqual([["Settings", "/settings"]]);
    expect(trail("/settings/themes/new")).toEqual([
      ["Settings", "/settings"],
      ["Themes", "/settings/themes"],
    ]);
  });

  it("leads back to Admin, and to the Admin page a deeper one belongs to", () => {
    expect(trail("/admin/webhooks")).toEqual([["Admin", "/admin"]]);
    expect(trail("/admin/access/groups")).toEqual([["Admin", "/admin"]]);
    expect(trail("/admin/templates/new")).toEqual([
      ["Admin", "/admin"],
      ["Templates", "/admin/templates"],
    ]);
  });

  it("puts the space, then Settings, above every section but General and what sits below one", () => {
    for (const path of ["/spaces/DOCS/settings/members", "/spaces/DOCS/settings/templates/new", "/spaces/DOCS/settings/general/stale"]) {
      expect(trail(path, { spaceName: "Docs" })).toEqual([
        ["Spaces", "/spaces"],
        ["Docs", "/spaces/DOCS"],
        ["Settings", "/spaces/DOCS/settings"],
      ]);
    }
    expect(trail("/spaces/DOCS/settings/general", { spaceName: "Docs" })).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/spaces/DOCS"],
    ]);
  });
});

describe("the parts of an address", () => {
  it("are the space and the page it is about", () => {
    expect(spaceOfPath("/spaces/DOCS/p/p-2/runbook")).toBe("DOCS");
    expect(pageOfPath("/spaces/DOCS/p/p-2/runbook")).toBe("p-2");
    expect(spaceOfPath("/spaces")).toBeUndefined();
    expect(spaceOfPath("/spaces/new/personal")).toBeUndefined();
    expect(spaceOfPath("/spaces/import")).toBeUndefined();
    expect(pageOfPath("/spaces/DOCS")).toBeUndefined();
  });
});
