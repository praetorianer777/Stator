import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ExciterAccount, ExciterRequirement } from "@/api/exciter";
import { DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { renderAt, standOnHintPage, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { ExciterAccountSection } from "./AccountSection";
import { normalizeRequirementKey, requirementBatches, requirementKeysOf, requirementUrl, TYPED_REQUIREMENT_KEY } from "./requirementKeys";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const BASE = "https://exciter.example.com";

const requirement = (key: string, title: string, over: Partial<ExciterRequirement> = {}): ExciterRequirement => ({
  id: "0195f000-0000-7000-8000-0000000000a1",
  key,
  url: requirementUrl(BASE, key),
  projectKey: key.slice(0, key.lastIndexOf("-R")),
  title,
  status: { name: "Approved", category: "approved" },
  revision: 3,
  trashedAt: null,
  ...over,
});

const account = (over: Partial<ExciterAccount>): Answer => ({
  status: 200,
  body: {
    account: {
      configured: true,
      baseUrl: BASE,
      connected: true,
      via: "token",
      status: "ok",
      user: null,
      checkedAt: null,
      missing: [],
      signInRefusal: null,
      ...over,
    },
  },
});

const para = (...keys: string[]): Doc => ({
  type: "doc",
  content: [
    {
      type: "paragraph",
      content: keys.flatMap((key) => [
        { type: "exciterRequirement", attrs: { key } },
        { type: "text", text: " " },
      ]),
    },
  ],
});

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <main>
        <h1>Page</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

const chips = () => Array.from(document.querySelectorAll<HTMLElement>("[data-exciter-requirement]"));

describe("requirement keys", () => {
  it("are Exciter's shape, upper case", () => {
    expect(normalizeRequirementKey(" chk-r42 ")).toBe("CHK-R42");
    for (const bad of ["CHK-42", "CHK-R0", "CHK-R042", "C-R1", "CP-12", "CHK-R", 42]) expect(normalizeRequirementKey(bad)).toBeNull();
    expect(TYPED_REQUIREMENT_KEY.exec("see CHK-R4 ")?.[1]).toBe("CHK-R4");
    expect(TYPED_REQUIREMENT_KEY.exec("aCHK-R4 ")).toBeNull();
  });

  it("open under their project in Exciter", () => {
    expect(requirementUrl(`${BASE}/`, "CHK-R4")).toBe(`${BASE}/p/CHK/r/CHK-R4`);
  });

  it("are read from a document once each, and asked in sorted batches", () => {
    expect(requirementKeysOf(para("CHK-R2", "CHK-R1", "CHK-R2"))).toEqual(["CHK-R2", "CHK-R1"]);
    expect(requirementBatches(["CHK-R2", "CHK-R1"])).toEqual([["CHK-R1", "CHK-R2"]]);
  });
});

describe("Exciter requirement chips in the read view", () => {
  it("show key, title, status and revision as the viewer may see them, from one lookup", async () => {
    const sent = stubApi({
      "GET /exciter/account": account({}),
      "GET /exciter/requirements": {
        status: 200,
        body: {
          status: "ok",
          requirements: [
            { key: "CHK-R1", requirement: requirement("CHK-R1", "The basket keeps its items for 30 days") },
            { key: "CHK-R2", requirement: null },
            { key: "OLD-R7", requirement: requirement("CHK-R4", "An order is confirmed by mail", { revision: 2 }) },
          ],
        },
      },
    });
    wrap(<DocView doc={para("CHK-R1", "CHK-R2", "OLD-R7")} />);
    const link = await screen.findByRole("link", { name: /CHK-R1.*The basket keeps its items/ });
    expect(link).toHaveAttribute("href", `${BASE}/p/CHK/r/CHK-R1`);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link.querySelector("[data-status-category='approved']")).toHaveTextContent("Approved");
    expect(link).toHaveTextContent("Revision 3");
    expect(chips().map((chip) => chip.dataset.state)).toEqual(["requirement", "hidden", "requirement"]);
    expect(chips()[1]).toHaveTextContent("CHK-R2Not visible to you");
    // A former key reads the requirement under the key it has now.
    expect(chips()[2]).toHaveTextContent("CHK-R4");
    expect(chips()[2]).toHaveAttribute("href", `${BASE}/p/CHK/r/CHK-R4`);
    expect(sent.filter((request) => request.path === "/exciter/requirements")).toHaveLength(1);
    expect(await axeViolations()).toEqual([]);
  });

  it("mark a requirement in Exciter's trash", async () => {
    stubApi({
      "GET /exciter/account": account({}),
      "GET /exciter/requirements": {
        status: 200,
        body: { status: "ok", requirements: [{ key: "CHK-R3", requirement: requirement("CHK-R3", "Gift wrapping", { trashedAt: "2026-09-03T09:00:00Z" }) }] },
      },
    });
    wrap(<DocView doc={para("CHK-R3")} />);
    await waitFor(() => expect(chips()[0]).toHaveAttribute("data-state", "trashed"));
    expect(chips()[0]).toHaveTextContent("In the trash");
  });

  it("show the key as a link and the way to connect to a viewer without a token, and ask Exciter nothing", async () => {
    standOnHintPage();
    const sent = stubApi({ "GET /exciter/account": account({ connected: false, status: "not_connected" }) });
    wrap(<DocView doc={para("CHK-R1")} />);
    const key = await screen.findByRole("link", { name: "CHK-R1" });
    expect(key).toHaveAttribute("href", `${BASE}/p/CHK/r/CHK-R1`);
    const hint = screen.getByRole("link", { name: "Connect Exciter to see this requirement" });
    expect(hint).toHaveAttribute("href", "/settings/profile?return=%2Fspaces%2Feng%2Fplan%3Ftab%3Dissues#exciter");
    expect(sent.some((request) => request.path.startsWith("/exciter/requirements"))).toBe(false);
    expect(await axeViolations()).toEqual([]);
  });

  it("show the key alone, as text, when Stator has no Exciter", async () => {
    stubApi({ "GET /exciter/account": account({ configured: false, baseUrl: null, connected: false, status: "not_configured" }) });
    wrap(<DocView doc={para("CHK-R1")} />);
    await waitFor(() => expect(chips()[0]?.dataset.state).toBe("plain"));
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("say an Exciter without requirement-embed@1 needs updating, and ask it nothing", async () => {
    const sent = stubApi({ "GET /exciter/account": account({ missing: [{ feature: "requirements", capability: "requirement-embed@1" }] }) });
    wrap(<DocView doc={para("CHK-R1")} />);
    await waitFor(() => expect(chips()[0]?.dataset.state).toBe("outdated"));
    expect(chips()[0]?.querySelector("[data-exciter-outdated]")).toHaveAttribute("title", expect.stringContaining("requirement-embed@1"));
    expect(sent.some((r) => r.path === "/exciter/requirements")).toBe(false);
  });

  it("say when Exciter did not answer", async () => {
    stubApi({ "GET /exciter/account": account({}), "GET /exciter/requirements": { status: 200, body: { status: "unreachable", requirements: [] } } });
    wrap(<DocView doc={para("CHK-R1")} />);
    await waitFor(() => expect(chips()[0]?.dataset.state).toBe("unreachable"));
    expect(chips()[0]).toHaveTextContent("Exciter did not answer");
  });
});

describe("the Exciter section of the profile", () => {
  it("stores a token Exciter accepts and says whom it acts as", async () => {
    let connected = false;
    const sent = stubApi({
      "GET /exciter/account": () =>
        account(
          connected
            ? { user: { id: "0195f000-0000-7000-8000-0000000000b1", name: "Bob", email: "bob@suite.test" } }
            : { connected: false, status: "not_connected" },
        ),
      "PUT /exciter/account/token": () => {
        connected = true;
        return account({ user: { id: "0195f000-0000-7000-8000-0000000000b1", name: "Bob", email: "bob@suite.test" } });
      },
    });
    wrap(<ExciterAccountSection />);
    expect(await screen.findByRole("link", { name: /Open Exciter's access tokens/ })).toHaveAttribute("href", `${BASE}/settings/tokens`);
    await userEvent.type(screen.getByLabelText("Exciter token"), "exciter_pat_bob");
    await userEvent.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText("Connected as Bob (bob@suite.test).")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ token: "exciter_pat_bob" });
  });

  it("says why Exciter did not connect the person at sign-in, above the token form", async () => {
    const why = "Ada Lovelace has no Exciter account yet. Sign in to Exciter once, or ask an administrator to let them in.";
    stubApi({ "GET /exciter/account": account({ connected: false, via: null, status: "not_connected", signInRefusal: why }) });
    wrap(<ExciterAccountSection />);
    const said = await screen.findByText(why);
    expect(said.closest("[data-exciter-sign-in-refusal]")).toHaveTextContent("When you signed in, Exciter did not connect your account:");
    expect(screen.getByLabelText("Exciter token")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says an account was connected at sign-in", async () => {
    stubApi({
      "GET /exciter/account": account({ via: "sign_in", user: { id: "0195f000-0000-7000-8000-0000000000b1", name: "Bob", email: "bob@suite.test" } }),
    });
    wrap(<ExciterAccountSection />);
    expect(await screen.findByText("Connected when you signed in through single sign-on.")).toBeInTheDocument();
    expect(screen.queryByLabelText("Exciter token")).toBeNull();
  });

  it("says what to do when Stator has no Exciter", async () => {
    stubApi({ "GET /exciter/account": account({ configured: false, baseUrl: null, connected: false, status: "not_configured" }) });
    wrap(<ExciterAccountSection />);
    expect(await screen.findByText(/Ask an administrator to connect it under Settings, Exciter/)).toBeInTheDocument();
  });
});

describe("the Exciter settings", () => {
  it("check an address, save it with the webhook secret, and say what Exciter lacks", async () => {
    let saved: unknown = null;
    const partner = {
      status: "ok",
      version: "1.4.0",
      capabilities: ["requirement-embed@1", "webhooks@1"],
      missing: [{ feature: "lists", capability: "requirement-list@1" }],
    };
    const sent = stubApi({
      "GET /exciter/account": account({ connected: false, status: "not_connected" }),
      "GET /exciter/connection": () => ({ status: 200, body: { connection: saved } }),
      "POST /exciter/connection/check": { status: 200, body: { check: { baseUrl: BASE, partner } } },
      "PUT /exciter/connection": () => {
        saved = {
          baseUrl: BASE,
          webhookSecretSet: true,
          webhookUrl: "http://stator.test/api/v1/exciter/webhook",
          webhookTopics: ["requirement.created", "review.decided"],
          lastDelivery: null,
          connected: 0,
          updatedAt: "2026-10-19T09:00:00Z",
          partner,
          app: { clientId: null, secretSet: false, offered: false, exchanging: false },
        };
        return { status: 200, body: { connection: saved } };
      },
    });
    await renderAt("/settings/exciter");
    await userEvent.type(await screen.findByLabelText("Exciter address"), BASE);
    await userEvent.click(screen.getByRole("button", { name: "Check" }));
    expect(await screen.findByText(/Exciter 1.4.0 answers at this address. It lacks 1 feature/)).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Webhook secret"), "exciter_whs_x");
    await userEvent.click(screen.getByRole("button", { name: "Connect Exciter" }));
    expect(await screen.findByText(/Stator turns off requirement lists, because Exciter does not offer requirement-list@1/)).toBeInTheDocument();
    expect(screen.getByDisplayValue("http://stator.test/api/v1/exciter/webhook")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ baseUrl: BASE, webhookSecret: "exciter_whs_x" });
  });

  it("store the partner app Exciter registered, which connects people at sign-in", async () => {
    let app = { clientId: null as string | null, secretSet: false, offered: true, exchanging: false };
    const connection = () => ({
      baseUrl: BASE,
      webhookSecretSet: true,
      webhookUrl: "http://stator.test/api/v1/exciter/webhook",
      webhookTopics: ["requirement.created"],
      lastDelivery: null,
      connected: 0,
      updatedAt: "2026-10-19T09:00:00Z",
      partner: { status: "ok", version: "1.5.0", capabilities: ["oauth.token-exchange@1", "requirement-embed@1"], missing: [] },
      app,
    });
    const sent = stubApi({
      "GET /exciter/account": account({ connected: false, status: "not_connected" }),
      "GET /exciter/connection": () => ({ status: 200, body: { connection: connection() } }),
      "PUT /exciter/connection/app": () => {
        app = { clientId: "0199-stator", secretSet: true, offered: true, exchanging: true };
        return { status: 200, body: { connection: connection() } };
      },
    });
    await renderAt("/settings/exciter");
    expect(await screen.findByText(/Exciter offers connecting people at sign-in/)).toBeInTheDocument();
    const form = screen.getByRole("form", { name: "Partner app" });
    await userEvent.type(within(form).getByLabelText("Client id"), "0199-stator");
    await userEvent.type(within(form).getByLabelText("Client secret"), "exciter_ocs_x");
    await userEvent.click(within(form).getByRole("button", { name: "Save the app" }));
    expect(await screen.findByText("People are connected to Exciter when they sign in.")).toBeInTheDocument();
    expect(screen.getByText("Client id 0199-stator.")).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT")?.body).toEqual({ clientId: "0199-stator", clientSecret: "exciter_ocs_x" });
    expect(await axeViolations()).toEqual([]);
  });
});
