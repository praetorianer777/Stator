import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ExampleSpaceJob } from "@/api/exampleSpace";
import type * as config from "@/config";
import { arrival, renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

// The page follows the worker by the clock; these tests run its clock fast.
vi.mock("@/config", async (original) => ({
  ...(await original<typeof config>()),
  EXAMPLE_SPACE_POLL_MS: 20,
  EXAMPLE_SPACE_SLOW_MS: 100,
  EXAMPLE_SPACE_GIVE_UP_MS: 400,
}));

afterEach(() => vi.unstubAllGlobals());

const member = { ...signedIn, organization: { ...signedIn.organization!, role: "member" as const } };
const example = aSpace({ id: "s-ex", key: "STATOR", name: "Getting to know Stator", description: "" });
const home = aPage({ spaceKey: "STATOR" });

function aJob(over: Partial<ExampleSpaceJob> = {}): ExampleSpaceJob {
  return {
    id: "j-1",
    state: "queued",
    language: "en",
    spaceKey: null,
    failure: null,
    message: null,
    requestedAt: "2026-10-07T10:00:00Z",
    startedAt: null,
    finishedAt: null,
    ...over,
  };
}

const opened = {
  "GET /spaces/STATOR": { status: 200, body: { space: example } },
  [`GET /pages/${example.homePageId}`]: { status: 200, body: { page: home, space: example } },
};

describe("the example space on the spaces overview", () => {
  it("is asked of the worker in the interface's language, followed while it is made, and opened", async () => {
    let asked = false;
    let looks = 0;
    const sent = stubApi({
      "GET /spaces": { status: 200, body: { spaces: [] } },
      "GET /example-space": () => {
        if (!asked) return { status: 200, body: { space: null, job: null } };
        looks++;
        if (looks < 3) return { status: 200, body: { space: null, job: aJob({ state: "running" }) } };
        return { status: 200, body: { space: example, job: aJob({ state: "done", spaceKey: "STATOR" }) } };
      },
      "POST /example-space": () => {
        asked = true;
        return { status: 202, body: { space: null, job: aJob() } };
      },
      ...opened,
    });
    const router = await renderAt("/spaces");
    await userEvent.click(await screen.findByRole("button", { name: "Create the example space" }));
    expect(await screen.findByRole("status")).toHaveTextContent("The example space is being made.");
    expect(screen.getByRole("button", { name: "Creating the example space" })).toBeDisabled();
    await arrival(router, "/s/STATOR");
    const asks = sent.filter((r) => r.method === "POST" && r.path === "/example-space");
    expect(asks.map((r) => r.body)).toEqual([{ language: "en" }]);
  });

  it("follows a making somebody began before the page opened, and offers no second one", async () => {
    let looks = 0;
    const sent = stubApi({
      "GET /spaces": { status: 200, body: { spaces: [] } },
      "GET /example-space": () =>
        ++looks < 3
          ? { status: 200, body: { space: null, job: aJob({ state: "running" }) } }
          : { status: 200, body: { space: example, job: aJob({ state: "done", spaceKey: "STATOR" }) } },
      ...opened,
    });
    const router = await renderAt("/spaces");
    expect(await screen.findByRole("button", { name: "Creating the example space" })).toBeDisabled();
    await arrival(router, "/s/STATOR");
    expect(sent.some((r) => r.method === "POST" && r.path === "/example-space")).toBe(false);
  });

  it("says when the worker seems not to run, and stops asking after a while with what to do", async () => {
    const sent = stubApi({
      "GET /spaces": { status: 200, body: { spaces: [] } },
      "GET /example-space": { status: 200, body: { space: null, job: aJob() } },
    });
    await renderAt("/spaces");
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("The worker that makes it may not be running"));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("has not been made after a quarter of an hour"));
    const looked = sent.filter((r) => r.path === "/example-space").length;
    await new Promise((settle) => setTimeout(settle, 100));
    expect(sent.filter((r) => r.path === "/example-space")).toHaveLength(looked);
    expect(await axeViolations()).toEqual([]);
  });

  it("says why the last making failed and offers it again", async () => {
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [] } },
      "GET /example-space": { status: 200, body: { space: null, job: aJob({ state: "failed", failure: "keys_taken" }) } },
    });
    await renderAt("/spaces");
    expect(await screen.findByText(/The keys STATOR to STATOR9 are all taken by other spaces/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create the example space" })).toBeEnabled();
  });

  it("says where it is when somebody made it in the meantime", async () => {
    stubApi({
      "GET /spaces": { status: 200, body: { spaces: [] } },
      "GET /example-space": { status: 200, body: { space: null, job: null } },
      "POST /example-space": { status: 200, body: { space: example, job: null } },
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
      "GET /example-space": { status: 200, body: { space: example, job: aJob({ state: "done", spaceKey: "STATOR" }) } },
    });
    const router = await renderAt("/spaces");
    await screen.findByRole("link", { name: "Getting to know Stator" });
    expect(screen.queryByRole("button", { name: "Create the example space" })).toBeNull();
    expect(router.state.location.pathname).toBe("/spaces");
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
    stubApi({ "GET /example-space": { status: 200, body: { space: { ...example, archivedAt: "2026-10-01T00:00:00Z" }, job: null } } });
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
    stubApi({ "GET /example-space": { status: 200, body: { space: null, job: null } } });
    const router = await renderAt("/settings/example-space");
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Example space" }));
    await arrival(router, "/settings/example-space");
  });
});
