import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { answerConfirm, confirmDialog, renderAt, signedIn, stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => vi.unstubAllGlobals());

const person = (userId: string, name: string, email: string, active = true) => ({
  userId,
  email,
  name,
  role: "member",
  active,
  joinedAt: "2026-09-01T08:00:00Z",
  guestSpace: null,
  roles: [],
});
const ada = person(signedIn.user.id, "Ada Lovelace", "ada@stator.test");
const bob = "0195f000-0000-7000-8000-0000000000b0";

describe("the Users page", () => {
  it("switches an account off after asking, and back on", async () => {
    let active = true;
    const sent = stubApi({
      "GET /users/requests": { status: 200, body: { requests: [] } },
      "GET /users": () => ({ status: 200, body: { members: [ada, person(bob, "Bob Builder", "bob@stator.test", active)] } }),
      [`PATCH /users/${bob}`]: async (request) => {
        active = ((await request.json()) as { active: boolean }).active;
        return { status: 204 };
      },
    });
    await renderAt("/admin/users");
    const row = (await screen.findByText("bob@stator.test")).closest("tr")!;
    expect(within(row).getByText("Can sign in")).toBeInTheDocument();
    expect(within((await screen.findByText("ada@stator.test")).closest("tr")!).queryByRole("button")).toBeNull();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(row).getByRole("button", { name: "Switch Bob Builder off" }));
    expect(await confirmDialog()).toHaveTextContent("cannot sign in any more");
    await answerConfirm(true);
    expect(await screen.findByText("Bob Builder was switched off.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PATCH")?.body).toEqual({ active: false });
    const off = (await screen.findByText("Switched off")).closest("tr")!;
    await userEvent.click(within(off).getByRole("button", { name: "Switch Bob Builder back on" }));
    await waitFor(() => expect(sent.filter((r) => r.method === "PATCH").at(-1)?.body).toEqual({ active: true }));
    expect(await screen.findByText("Bob Builder was switched back on.")).toBeInTheDocument();
  });

  it("shows the server's refusal as it came", async () => {
    stubApi({
      "GET /users/requests": { status: 200, body: { requests: [] } },
      "GET /users": { status: 200, body: { members: [ada, person(bob, "Bob Builder", "bob@stator.test")] } },
      [`PATCH /users/${bob}`]: {
        status: 409,
        body: {
          error: {
            code: "conflict",
            message: "Stator keeps at least one active administrator. Make somebody else an administrator first, then take it from this one.",
          },
        },
      },
    });
    await renderAt("/admin/users");
    await userEvent.click(await screen.findByRole("button", { name: "Switch Bob Builder off" }));
    await answerConfirm(true);
    expect((await screen.findAllByText(/keeps at least one active administrator/)).length).toBeGreaterThan(0);
  });
});
