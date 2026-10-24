import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Consent } from "@/api/oauth";
import { leave } from "@/lib/leave";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";

vi.mock("@/lib/leave", () => ({ leave: vi.fn() }));

afterEach(() => {
  vi.unstubAllGlobals();
  vi.mocked(leave).mockReset();
});

const QUERY = "response_type=code&client_id=0199-rotor&redirect_uri=https%3A%2F%2Frotor.example%2Fcb&state=s1&code_challenge=abc&code_challenge_method=S256";
const PATH = `/oauth/authorize?${QUERY}`;
const BACK = "https://rotor.example/cb?code=stator_oac_x&iss=https%3A%2F%2Fwiki.example&state=s1";

function consent(over: Partial<Consent> = {}): Consent {
  return {
    client: { id: "0199-rotor", name: "Rotor", clientUri: "https://rotor.example" },
    redirectHost: "rotor.example",
    person: "Ada Lovelace",
    scopes: ["read", "write"],
    pending: false,
    allowsApp: false,
    mayAllow: true,
    mayCancel: true,
    ...over,
  };
}

const spaces = {
  status: 200,
  body: {
    spaces: [
      { id: "s-eng", key: "ENG", name: "Engineering" },
      { id: "s-ops", key: "OPS", name: "Operations" },
    ],
  },
};

function stub(answer: Answer | ((request: Request) => Answer), decided: Answer = { status: 200, body: { redirectTo: BACK } }) {
  return stubApi({ "GET /oauth/consent": answer, "POST /oauth/consent": decided, "GET /spaces": spaces });
}

describe("the consent page", () => {
  it("asks for the query exactly as the app sent it, and names the app, where it goes back and what it asks", async () => {
    let asked = "";
    stub((request) => {
      asked = new URL(request.url).search;
      return { status: 200, body: { consent: consent() } };
    });
    await renderAt(PATH);
    expect(await screen.findByRole("heading", { level: 1, name: "Connect Rotor" })).toBeInTheDocument();
    expect(asked).toBe(`?${QUERY}`);
    expect(screen.getByText("Rotor wants to act as Ada Lovelace in Stator, at its MCP endpoint and nowhere else.")).toBeInTheDocument();
    expect(screen.getByText("Afterwards you are sent back to rotor.example.")).toBeInTheDocument();
    expect(document.querySelector("[data-consent-address]")).toHaveTextContent("https://rotor.example");
    expect(screen.getByRole("radio", { name: /Read and change pages/ })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("radio", { name: /Every space you can see/ })).toHaveAttribute("aria-checked", "true");
    expect(await axeViolations()).toEqual([]);
  });

  it("offers a member nothing to allow while an administrator has not allowed the app", async () => {
    const refusal = "A Stator administrator must allow Rotor first. Ask one to connect it once; then you can connect it too.";
    stub({ status: 200, body: { consent: consent({ pending: true, mayAllow: false, mayCancel: false, refusal }) } });
    await renderAt(PATH, { me: { ...signedIn, organization: { name: "Demo", role: "member", guestSpace: null } } });
    expect(await screen.findByText(refusal)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Allow" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
    expect(document.querySelector("[data-consent-allows-app]")).toBeNull();
  });

  it("tells an administrator that allowing a waiting app lets every member connect it", async () => {
    stub({ status: 200, body: { consent: consent({ pending: true, allowsApp: true }) } });
    await renderAt(PATH);
    expect(await screen.findByText(/As an administrator, allowing it here lets every member connect it too/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Allow" })).toBeInTheDocument();
  });

  it("allows reading alone in the spaces chosen, and sends the browser back with the answer", async () => {
    const sent = stub({ status: 200, body: { consent: consent() } });
    const user = userEvent.setup();
    await renderAt(PATH);
    await user.click(await screen.findByRole("radio", { name: /Read only/ }));
    await user.click(screen.getByRole("radio", { name: /Only the spaces chosen here/ }));
    await user.click(screen.getByRole("button", { name: "Allow" }));
    expect(await screen.findByText("Choose at least one space, or let it reach every space you can see.")).toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: /Operations/ }));
    await user.click(screen.getByRole("button", { name: "Allow" }));
    await waitFor(() => expect(leave).toHaveBeenCalledWith(BACK));
    expect(sent.find((r) => r.method === "POST" && r.path === "/oauth/consent")?.body).toEqual({
      query: QUERY,
      decision: "allow",
      scope: "read",
      spaces: ["OPS"],
    });
  });

  it("cancels with deny, which the app is told", async () => {
    const sent = stub({ status: 200, body: { consent: consent() } }, { status: 200, body: { redirectTo: "https://rotor.example/cb?error=access_denied" } });
    const user = userEvent.setup();
    await renderAt(PATH);
    await user.click(await screen.findByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(leave).toHaveBeenCalledWith("https://rotor.example/cb?error=access_denied"));
    expect(sent.find((r) => r.method === "POST")?.body).toEqual({ query: QUERY, decision: "deny" });
  });

  it("goes straight back when the API answers at the app at once", async () => {
    stub({ status: 200, body: { redirectTo: "https://rotor.example/cb?error=invalid_request" } });
    await renderAt(PATH);
    await waitFor(() => expect(leave).toHaveBeenCalledWith("https://rotor.example/cb?error=invalid_request"));
  });

  it("says why an app Stator does not know cannot be connected", async () => {
    const message = "Stator does not know this app, or the app asked to be sent back to an address it did not register. Ask its maker to register it again.";
    stub({ status: 400, body: { error: { code: "unknown_app", message } } });
    await renderAt(PATH);
    expect(await screen.findByRole("heading", { level: 1, name: "This app cannot be connected" })).toBeInTheDocument();
    expect(screen.getByText(message)).toBeInTheDocument();
    expect(leave).not.toHaveBeenCalled();
  });
});
