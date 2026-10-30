import { describe, expect, it } from "vitest";
import { crumbsFor, pageOfPath, spaceOfPath } from "./Crumbs";

const page = { id: "p-2", title: "Runbook", home: false, kind: "page" as const, ancestors: [{ id: "p-home", title: "Handbook", home: true }] };
const trail = (pathname: string, search: Record<string, unknown> = {}, names = {}) => crumbsFor(pathname, search, names).map(({ label, to }) => [label, to]);

describe("the trail above a page", () => {
  it("is empty on the places of the wiki and the settings hub", () => {
    for (const path of ["/", "/home", "/search", "/reviews", "/tasks", "/spaces", "/notifications", "/settings"]) expect(trail(path)).toEqual([]);
  });

  it("leads back to Spaces from a space's own pages", () => {
    expect(trail("/spaces/new")).toEqual([["Spaces", "/spaces"]]);
    expect(trail("/s/DOCS")).toEqual([["Spaces", "/spaces"]]);
    expect(trail("/s/DOCS/blog", {}, { spaceName: "Docs" })).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/s/DOCS"],
    ]);
  });

  it("names the space by its key until the space is known", () => {
    expect(trail("/s/DOCS/settings")).toEqual([
      ["Spaces", "/spaces"],
      ["DOCS", "/s/DOCS"],
    ]);
  });

  it("passes every page above a page, the home page by its space's name", () => {
    const names = { spaceName: "Docs", page };
    expect(trail("/s/DOCS/p/p-2/runbook", {}, names)).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/s/DOCS"],
    ]);
    expect(trail("/s/DOCS/p/p-2/runbook/edit", {}, names)).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/s/DOCS"],
    ]);
  });

  it("puts the page itself, and its history below it, above a version", () => {
    const names = { spaceName: "Docs", page };
    expect(trail("/s/DOCS/p/p-2/runbook/history", {}, names)).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/s/DOCS"],
      ["Runbook", "/s/DOCS/p/p-2/runbook"],
    ]);
  });

  it("puts a post in its space's blog", () => {
    const post = { ...page, kind: "post" as const, ancestors: [] };
    expect(trail("/s/DOCS/p/p-2/runbook", {}, { spaceName: "Docs", page: post })).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/s/DOCS"],
      ["Blog", "/s/DOCS/blog"],
    ]);
  });

  it("leads back to Settings, and to the settings page a deeper one belongs to", () => {
    expect(trail("/settings/tokens")).toEqual([["Settings", "/settings"]]);
    expect(trail("/settings/themes/new")).toEqual([
      ["Settings", "/settings"],
      ["Themes", "/settings/themes"],
    ]);
  });

  it("leads a space's template back to the space's settings", () => {
    expect(trail("/settings/templates/new", { space: "DOCS" }, { spaceName: "Docs" })).toEqual([
      ["Spaces", "/spaces"],
      ["Docs", "/s/DOCS"],
      ["Space settings", "/s/DOCS/settings"],
    ]);
  });
});

describe("the parts of an address", () => {
  it("are the space and the page it is about", () => {
    expect(spaceOfPath("/s/DOCS/p/p-2/runbook")).toBe("DOCS");
    expect(pageOfPath("/s/DOCS/p/p-2/runbook")).toBe("p-2");
    expect(spaceOfPath("/spaces")).toBeUndefined();
    expect(pageOfPath("/s/DOCS")).toBeUndefined();
  });
});
