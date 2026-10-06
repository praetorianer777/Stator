import { afterEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { arrival, renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => vi.unstubAllGlobals());

const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const } };
const example = aSpace({ id: "s-ex", key: "STATOR", name: "Getting to know Stator", description: "" });
const home = aPage({ spaceKey: "STATOR" });

describe("the example space on the spaces overview", () => {
  it("is offered to an administrator while there is none, made in the interface's language and opened", async () => {
    const sent = stubApi({
      "GET /spaces": { status: 200, body: { spaces: [] } },
      "GET /example-space": { status: 200, body: { space: null } },
      "POST /example-space": { status: 201, body: { space: example, created: true } },
      "GET /spaces/STATOR": { status: 200, body: { space: example } },
      [`GET /pages/${example.homePageId}`]: { status: 200, body: { page: home, space: example } },
    });
    const router = await renderAt("/spaces");
    await userEvent.click(await screen.findByRole("button", { name: "Create the example space" }));
    await arrival(router, "/s/STATOR");
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ language: "en" });
  });

  it("says where it is when somebody made it in the meantime", async () => {
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [] } },
      "GET /example-space": { status: 200, body: { space: null } },
      "POST /example-space": { status: 200, body: { space: example, created: false } },
    });
    await renderAt("/spaces");
    await userEvent.click(await screen.findByRole("button", { name: "Create the example space" }));
    const said = await screen.findByRole("status");
    expect(said).toHaveTextContent("The example space exists already:");
    expect(screen.getByRole("link", { name: "Getting to know Stator" })).toHaveAttribute("href", "/s/STATOR");
    expect(await axeViolations()).toEqual([]);
  });

  it("is not offered once there is one, nor to a member, who is never asked about it", async () => {
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [example] } },
      "GET /example-space": { status: 200, body: { space: example } },
    });
    await renderAt("/spaces");
    await screen.findByRole("link", { name: "Getting to know Stator" });
    expect(screen.queryByRole("button", { name: "Create the example space" })).toBeNull();
    vi.unstubAllGlobals();

    const sent = stubApi({ "GET /spaces": { status: 200, body: { spaces: [] } } });
    await renderAt("/spaces", { me: member });
    await screen.findByText(/Ask an administrator of your organization to create one/);
    expect(screen.queryByRole("button", { name: "Create the example space" })).toBeNull();
    expect(sent.some((r) => r.path === "/example-space")).toBe(false);
  });
});

describe("the example space in the organization's settings", () => {
  it("names the one there is and still offers to make it", async () => {
    stubApi({ "GET /example-space": { status: 200, body: { space: { ...example, archivedAt: "2026-10-01T00:00:00Z" } } } });
    await renderAt("/settings/example-space");
    expect(await screen.findByRole("status")).toHaveTextContent("It is archived.");
    expect(screen.getByRole("button", { name: "Create the example space" })).toBeEnabled();
    expect(await axeViolations()).toEqual([]);
  });

  it("tells a member whom to ask", async () => {
    await renderAt("/settings/example-space", { me: member });
    expect(await screen.findByText(/Only administrators of the organization create the example space/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create the example space" })).toBeNull();
  });

  it("is in the account menu of an administrator", async () => {
    stubApi({ "GET /example-space": { status: 200, body: { space: null } } });
    const router = await renderAt("/settings/example-space");
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Example space" }));
    await arrival(router, "/settings/example-space");
  });
});
