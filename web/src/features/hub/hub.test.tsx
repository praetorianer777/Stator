import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { arrival, renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => vi.unstubAllGlobals());

const space = aSpace({ key: "NEWS", name: "News" });
const front = aPage({ id: "0195f000-0000-7000-8000-0000000000e1", title: "Front page", spaceKey: "NEWS", home: false });
const hubPage = { id: front.id, title: front.title, spaceKey: "NEWS" };
const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const } };

function stubHub(hub: Answer, more: Record<string, Answer> = {}) {
  return stubApi({
    "GET /org/hub": hub,
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/NEWS": { status: 200, body: { space } },
    [`GET /pages/${front.id}`]: { status: 200, body: { page: front, space } },
    "GET /spaces/NEWS/pages": { status: 200, body: { pages: [] } },
    "GET /spaces/NEWS/outline": {
      status: 200,
      body: {
        pages: [
          { id: space.homePageId, parentId: null, title: "News", depth: 0 },
          { id: front.id, parentId: space.homePageId, title: "Front page", depth: 1 },
        ],
      },
    },
    ...more,
  });
}

describe("the organization's hub", () => {
  it("is where everybody lands when it says so, with the reader's own home a link away", async () => {
    stubHub({ status: 200, body: { hub: { page: hubPage, landing: true } } });
    const router = await renderAt("/", { me: member });
    await arrival(router, `/s/NEWS/p/${front.id}/front-page`);
    const nav = screen.getAllByRole("navigation", { name: "Everywhere" })[0]!;
    expect(within(nav).getByRole("link", { name: "Hub" })).toHaveAttribute("href", `/s/NEWS/p/${front.id}/front-page`);
    expect(within(nav).getByRole("link", { name: "Home" })).toHaveAttribute("href", "/home");
  });

  it("is a link in the navigation, and nobody lands on it unless it says so", async () => {
    stubHub({ status: 200, body: { hub: { page: hubPage, landing: false } } });
    const router = await renderAt("/", { me: member });
    await arrival(router, "/");
    const nav = screen.getAllByRole("navigation", { name: "Everywhere" })[0]!;
    expect(await within(nav).findByRole("link", { name: "Hub" })).toBeInTheDocument();
    expect(within(nav).getByRole("link", { name: "Home" })).toHaveAttribute("href", "/");
  });

  it("is not in the navigation when there is none the reader may see", async () => {
    stubHub({ status: 200, body: { hub: { page: null, landing: false } } });
    const router = await renderAt("/", { me: member });
    await arrival(router, "/");
    await waitFor(() => expect(screen.getAllByRole("link", { name: "Home" }).length).toBeGreaterThan(0));
    expect(screen.queryByRole("link", { name: "Hub" })).toBeNull();
  });
});

describe("choosing the hub", () => {
  it("lets an administrator pick a page and make everybody land on it", async () => {
    const sent = stubHub(
      { status: 200, body: { hub: { page: null, landing: false } } },
      { "PUT /org/hub": { status: 200, body: { hub: { page: hubPage, landing: true } } } },
    );
    await renderAt("/settings/hub");
    expect(await screen.findByText("There is no hub page yet.")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText("Page")).not.toBeDisabled());
    await userEvent.selectOptions(screen.getByLabelText("Page"), front.id);
    await userEvent.click(screen.getByLabelText("Everybody lands on the hub when they open Stator"));
    await userEvent.click(screen.getByRole("button", { name: "Save hub" }));
    await waitFor(() => expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ pageId: front.id, landing: true }));
    expect(await screen.findByText(/everybody lands on it/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("tells a member who chooses it", async () => {
    stubHub({ status: 200, body: { hub: { page: null, landing: false } } });
    await renderAt("/settings/hub", { me: member });
    expect(await screen.findByText(/Only an administrator of the organization chooses its hub page/)).toBeInTheDocument();
  });
});
