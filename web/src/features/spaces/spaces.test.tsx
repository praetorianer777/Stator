import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { suggestKey } from "@/api/spaces";
import { SPACE_KEY_MAX_LENGTH } from "@/config";
import { renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => vi.unstubAllGlobals());

const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const } };
const home = aPage();
const space = aSpace();

describe("a suggested key", () => {
  it("takes the initials of several words and the start of one, as the API does", () => {
    const cases: Array<[string, string]> = [
      ["Engineering", "ENGINEERIN"],
      ["Team Handbook", "TH"],
      ["docs", "DOCS"],
      ["Q & A", "QA"],
      ["X", "XX"],
      ["2026 Plans", ""],
      ["", ""],
    ];
    for (const [name, key] of cases) expect(suggestKey(name, SPACE_KEY_MAX_LENGTH)).toBe(key);
  });
});

describe("the space directory", () => {
  it("lists the spaces and offers an administrator a new one", async () => {
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [space, aSpace({ id: "s2", key: "ENG", name: "Engineering", description: "" })] } } });
    await renderAt("/spaces");
    const row = (await screen.findByRole("link", { name: "Handbook" })).closest("tr")!;
    expect(row).toHaveTextContent("DOCS");
    expect(row).toHaveTextContent("How we work.");
    expect(screen.getByRole("link", { name: "Handbook" })).toHaveAttribute("href", "/s/DOCS");
    expect(screen.getByRole("button", { name: "Create space" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("tells a member who to ask when there is nothing yet", async () => {
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [] } } });
    await renderAt("/spaces", { me: member });
    expect(await screen.findByText(/Ask an administrator of your organization to create one/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create space" })).toBeNull();
  });
});

describe("creating a space", () => {
  it("suggests the key from the name and lands on the new home page", async () => {
    const sent = stubApi({
      "GET /spaces": { status: 200, body: { spaces: [] } },
      "POST /spaces": { status: 201, body: { space } },
      "GET /spaces/DOCS": { status: 200, body: { space } },
      [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    });
    await renderAt("/spaces/new");
    await userEvent.type(await screen.findByLabelText("Name"), "Team Handbook");
    expect(screen.getByLabelText("Key")).toHaveValue("TH");
    await userEvent.clear(screen.getByLabelText("Key"));
    await userEvent.type(screen.getByLabelText("Key"), "docs");
    expect(screen.getByLabelText("Key")).toHaveValue("DOCS");
    await userEvent.type(screen.getByLabelText("Description"), "How we work.");
    await userEvent.click(screen.getByRole("button", { name: "Create space" }));

    expect(await screen.findByText("Welcome to the handbook.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ name: "Team Handbook", key: "DOCS", description: "How we work." });
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Handbook");
    expect(await axeViolations()).toEqual([]);
  });

  it("shows what the API refused under the field it refused", async () => {
    stubApi({
      "POST /spaces": {
        status: 422,
        body: {
          error: {
            code: "validation_failed",
            message: "Some fields need attention.",
            fields: { key: "The key DOCS is taken by another space. Choose another." },
          },
        },
      },
    });
    await renderAt("/spaces/new");
    await userEvent.type(await screen.findByLabelText("Name"), "Docs");
    await userEvent.click(screen.getByRole("button", { name: "Create space" }));
    expect(await screen.findByText("The key DOCS is taken by another space. Choose another.")).toBeInTheDocument();
    expect(screen.getByLabelText("Key")).toHaveAttribute("aria-invalid", "true");
  });
});

describe("a space's home", () => {
  it("shows the home page, with the space in the sidebar and an edit button", async () => {
    stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    });
    await renderAt("/s/docs");
    expect(await screen.findByText("Welcome to the handbook.")).toBeInTheDocument();
    const nav = document.querySelector<HTMLElement>("[data-space-nav]")!;
    expect(within(nav).getByRole("link", { name: "Space home" })).toHaveAttribute("href", "/s/DOCS");
    expect(within(nav).getByRole("link", { name: "Space settings" })).toHaveAttribute("href", "/s/DOCS/settings");
    expect(screen.getByRole("button", { name: "Edit" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says so in a sentence when the key leads nowhere", async () => {
    stubApi({
      "GET /spaces/NOPE": {
        status: 404,
        body: { error: { code: "not_found", message: "That space was not found. Check the key in the address; the space may have been deleted." } },
      },
    });
    await renderAt("/s/NOPE");
    expect(await screen.findByText("That space was not found. Check the key in the address; the space may have been deleted.")).toBeInTheDocument();
  });
});

describe("space settings", () => {
  it("let an administrator rename the space and explain who may do what", async () => {
    const renamed = aSpace({ name: "Team handbook", updatedAt: "2026-09-29T09:00:00Z" });
    const sent = stubApi({
      "GET /spaces/DOCS": { status: 200, body: { space } },
      "PATCH /spaces/DOCS": { status: 200, body: { space: renamed } },
      "GET /spaces/DOCS/permissions": { status: 200, body: { grants: [] } },
    });
    await renderAt("/s/DOCS/settings");
    const name = await screen.findByLabelText("Name");
    await userEvent.clear(name);
    await userEvent.type(name, "Team handbook");
    await userEvent.click(screen.getByRole("button", { name: "Save details" }));
    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PATCH")?.body).toEqual({ name: "Team handbook", description: "How we work." });
    expect(screen.getByRole("button", { name: "Delete space" })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("tab", { name: "Permissions" }));
    expect(await screen.findByText(/Who may do what in this space/)).toBeInTheDocument();
  });

  it("show a member the details without letting them change them", async () => {
    stubApi({
      "GET /spaces/DOCS": {
        status: 200,
        body: { space: aSpace({ can: { editPages: true, administer: false, delete: false, purgeTrash: false, addComments: true, deletePages: true } }) },
      },
    });
    await renderAt("/s/DOCS/settings", { me: member });
    expect(
      await screen.findByText("Only an administrator of this space can change its details. Ask one of them, or an administrator of the organization."),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveAttribute("readonly");
    expect(screen.queryByRole("button", { name: "Save details" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete space" })).toBeNull();
  });
});
