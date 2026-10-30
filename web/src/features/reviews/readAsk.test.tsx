import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Page } from "@/api/pages";
import type { ReadConfirmations } from "@/api/reviews";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const home = aPage();
const PAGE_ID = "0195f000-0000-7000-8000-0000000000a2";
const PATH = `/spaces/DOCS/p/${PAGE_ID}/policy`;
const reader = { edit: false, delete: false, restrict: false, comment: true, archive: false, add: false, grantEdit: false };
const readerSpace = aSpace({ can: { editPages: false, administer: false, delete: false, purgeTrash: false, addComments: true, deletePages: false } });

function policy(over: Partial<Page> = {}) {
  return aPage({
    id: PAGE_ID,
    title: "Policy",
    home: false,
    parentId: home.id,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    version: 4,
    ...over,
  });
}

type Answers = Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)>;

function stubPage(page: Page | (() => Page), more: Answers = {}, space = aSpace()) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${PAGE_ID}`]: () => ({ status: 200, body: { page: typeof page === "function" ? page() : page, space } }),
    [`GET /pages/${PAGE_ID}/attachments`]: { status: 200, body: { attachments: [] } },
    [`POST /pages/${PAGE_ID}/visit`]: { status: 204 },
    ...more,
  });
}

const readers: ReadConfirmations = {
  since: 3,
  confirmed: [
    { person: { id: "p1", name: "Carol Reader", email: "carol@stator.test" }, version: 4, confirmedAt: "2026-10-05T09:00:00Z" },
    { person: { id: "p2", name: "Dan Reader", email: "dan@stator.test" }, version: 3, confirmedAt: "2026-10-04T09:00:00Z" },
  ],
  pending: [{ id: "p3", name: "Eve Late", email: "eve@stator.test" }],
  more: 7,
};

describe("a page that asks its readers to confirm", () => {
  it("asks a reader who has not, and once they confirm says when", async () => {
    const sent = stubPage(
      policy({ can: reader, readAsk: { since: 3, confirmedAt: null } }),
      {
        [`POST /pages/${PAGE_ID}/read-confirmations`]: { status: 201, body: { readAsk: { since: 3, confirmedAt: "2026-10-11T09:30:00Z" } } },
      },
      readerSpace,
    );
    await renderAt(PATH);
    const banner = (await screen.findByText("Please confirm you have read this page.")).closest("[data-read-ask-banner]") as HTMLElement;
    expect(banner).toHaveAttribute("data-read-ask-banner", "ask");
    expect(screen.queryByRole("button", { name: /^Read by/ })).toBeNull();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(banner).getByRole("button", { name: "I have read it" }));
    expect(await screen.findByText(/^You confirmed you read this page on/)).toBeInTheDocument();
    expect(document.querySelector("[data-read-ask-banner]")).toHaveAttribute("data-read-ask-banner", "confirmed");
    expect(sent.filter((r) => r.method === "POST" && r.path === `/pages/${PAGE_ID}/read-confirmations`)).toHaveLength(1);
  });

  it("shows the server's refusal when the page stopped asking meanwhile", async () => {
    const refusal = "This page no longer asks its readers to confirm. Reload it to see it as it is.";
    stubPage(
      policy({ can: reader, readAsk: { since: 3, confirmedAt: null } }),
      {
        [`POST /pages/${PAGE_ID}/read-confirmations`]: { status: 409, body: { error: { code: "not_asked", message: refusal } } },
      },
      readerSpace,
    );
    await renderAt(PATH);
    await userEvent.click(await screen.findByRole("button", { name: "I have read it" }));
    expect(await screen.findByText(refusal)).toBeInTheDocument();
  });

  it("asks nothing of a reader who already confirmed, nor on a page that does not ask", async () => {
    stubPage(policy({ can: reader }), {}, readerSpace);
    await renderAt(PATH);
    await screen.findByText("Welcome to the handbook.");
    expect(document.querySelector("[data-read-ask-banner]")).toBeNull();
  });

  it("shows its editors who read it, with when and which version, and who has not yet", async () => {
    stubPage(policy({ readAsk: { since: 3, confirmedAt: "2026-10-05T09:00:00Z" } }), {
      [`GET /pages/${PAGE_ID}/read-confirmations`]: { status: 200, body: { readers } },
    });
    await renderAt(PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Read by 2" }));
    const dialog = screen.getByRole("dialog", { name: "Who read Policy" });
    expect(within(dialog).getByText("Confirmations count from version 3 on.")).toBeInTheDocument();
    const confirmed = within(dialog).getByRole("region", { name: "Confirmed" });
    expect(within(confirmed).getByText("Carol Reader")).toBeInTheDocument();
    expect(within(confirmed).getByText(/, version 4$/)).toBeInTheDocument();
    const notYet = within(dialog).getByRole("region", { name: "Not yet" });
    expect(within(notYet).getByText("Eve Late")).toBeInTheDocument();
    expect(within(notYet).getByText("and 7 more")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});

describe("asking readers to confirm", () => {
  async function menu() {
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
  }

  it("is an editor's, from the page actions", async () => {
    let since: number | null = null;
    const sent = stubPage(() => policy({ readAsk: { since, confirmedAt: null } }), {
      [`PUT /pages/${PAGE_ID}/read-ask`]: () => {
        since = 4;
        return { status: 200, body: { readAsk: { since: 4, confirmedAt: null } } };
      },
      [`GET /pages/${PAGE_ID}/read-confirmations`]: { status: 200, body: { readers: { since: 4, confirmed: [], pending: [], more: 0 } } },
    });
    await renderAt(PATH);
    await menu();
    expect(screen.queryByRole("menuitem", { name: "Stop asking readers to confirm" })).toBeNull();
    await userEvent.click(screen.getByRole("menuitem", { name: "Ask readers to confirm" }));
    expect(await screen.findByText("Please confirm you have read this page.")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Read by 0" })).toBeInTheDocument();
    expect(sent.find((r) => r.method === "PUT" && r.path === `/pages/${PAGE_ID}/read-ask`)!.body).toEqual({ ask: true });
  });

  it("asks again or stops asking on a page that asks", async () => {
    const sent = stubPage(policy({ readAsk: { since: 3, confirmedAt: "2026-10-05T09:00:00Z" } }), {
      [`PUT /pages/${PAGE_ID}/read-ask`]: async (request) => {
        const { ask } = (await request.json()) as { ask: boolean };
        return { status: 200, body: { readAsk: ask ? { since: 4, confirmedAt: null } : { since: null, confirmedAt: null } } };
      },
      [`GET /pages/${PAGE_ID}/read-confirmations`]: { status: 200, body: { readers } },
    });
    await renderAt(PATH);
    await menu();
    expect(screen.queryByRole("menuitem", { name: "Ask readers to confirm" })).toBeNull();
    await userEvent.click(screen.getByRole("menuitem", { name: "Ask readers to confirm again" }));
    expect(await screen.findByText("Please confirm you have read this page.")).toBeInTheDocument();
    await menu();
    await userEvent.click(screen.getByRole("menuitem", { name: "Stop asking readers to confirm" }));
    await waitFor(() => expect(document.querySelector("[data-read-ask-banner]")).toBeNull());
    expect(sent.filter((r) => r.method === "PUT").map((r) => r.body)).toEqual([{ ask: true }, { ask: false }]);
  });
});
