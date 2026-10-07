import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Page } from "@/api/pages";
import type { Owner, Verification } from "@/api/stewardship";
import { VERIFY_DEFAULT_DAYS, VERIFY_TERM_DAYS } from "@/config";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const home = aPage();
const pageId = "0195f000-0000-7000-8000-0000000000c1";
const PATH = `/s/DOCS/p/${pageId}/runbook`;
const ann: Owner = { id: "0195f000-0000-7000-8000-0000000000a1", name: "Ann Owner", canView: true };
const verified: Verification = {
  status: "verified",
  verifiedById: "0195f000-0000-7000-8000-0000000000b1",
  verifiedByName: "Ben Editor",
  verifiedAt: "2026-09-01T09:00:00Z",
  expiresAt: "2026-12-01T09:00:00Z",
  version: 1,
};

function stubPage(page: Partial<Page>, more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  const shown = aPage({
    id: pageId,
    title: "Runbook",
    home: false,
    parentId: home.id,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    ...page,
  });
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [aSpace()] } },
    "GET /spaces/DOCS": { status: 200, body: { space: aSpace() } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page: shown, space: aSpace() } },
    [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
    [`POST /pages/${pageId}/visit`]: { status: 204 },
    ...more,
  });
}

async function openFromMenu() {
  await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
  await userEvent.click(screen.getByRole("menuitem", { name: "Owner and verification" }));
  return screen.getByRole("dialog", { name: "Owner and verification of Runbook" });
}

describe("the page header", () => {
  it("shows the owner and a verified badge that opens who verified it", async () => {
    stubPage({
      owner: ann,
      verification: verified,
      can: { edit: false, delete: false, restrict: false, comment: true, archive: false, add: false, grantEdit: false },
    });
    await renderAt(PATH);
    expect(await screen.findByText("Owner: Ann Owner")).toBeInTheDocument();
    const badge = screen.getByRole("button", { name: /^Verified until/ });
    expect(badge).toHaveAttribute("data-verification-badge", "verified");
    expect(screen.queryByRole("button", { name: "Page actions" })).toBeInTheDocument();
    await userEvent.click(badge);
    const dialog = screen.getByRole("dialog", { name: "Owner and verification of Runbook" });
    expect(within(dialog).getByText(/^Verified by Ben Editor on/)).toBeInTheDocument();
    expect(within(dialog).getByText("Only people who may edit this page can change its owner or verify it.")).toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: "Verify again" })).toBeNull();
    expect(within(dialog).queryByRole("combobox")).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("says when a verification ran out, and that the page changed since it was checked", async () => {
    stubPage({ version: 3, verification: { ...verified, status: "expired" } });
    await renderAt(PATH);
    const badge = await screen.findByRole("button", { name: /^Verification expired on/ });
    expect(badge).toHaveAttribute("data-verification-badge", "expired");
    await userEvent.click(badge);
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/the verification ran out on/)).toBeInTheDocument();
    expect(within(dialog).getByText("The check was of version 1; the page is at version 3 now.")).toBeInTheDocument();
  });

  it("tells an editor when the owner can no longer view the page", async () => {
    stubPage({ owner: { ...ann, canView: false } });
    await renderAt(PATH);
    expect(await screen.findByText("(no longer has access)")).toBeInTheDocument();
    const dialog = await openFromMenu();
    expect(within(dialog).getByText("Ann Owner can no longer view this page and is not reminded. Choose somebody who can.")).toBeInTheDocument();
  });

  it("offers nothing to change on a page nobody else has read", async () => {
    stubPage({ unpublished: true, version: 0 });
    await renderAt(PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Page actions" }));
    expect(screen.queryByRole("menuitem", { name: "Owner and verification" })).toBeNull();
  });
});

describe("the owner and verification dialog", () => {
  it("names an owner from the people picker and removes them", async () => {
    let owner: Owner | null = null;
    const sent = stubPage(
      {},
      {
        "GET /people": { status: 200, body: { people: [{ id: ann.id, name: ann.name, email: "ann@example.test" }] } },
        [`PUT /pages/${pageId}/owner`]: () => {
          owner = ann;
          return { status: 200, body: { owner } };
        },
        [`DELETE /pages/${pageId}/owner`]: () => {
          owner = null;
          return { status: 204 };
        },
      },
    );
    await renderAt(PATH);
    const dialog = await openFromMenu();
    expect(within(dialog).getByText("Nobody owns this page yet.")).toBeInTheDocument();
    const picker = within(dialog).getByRole("combobox", { name: "Choose the owner" });
    await userEvent.type(picker, "ann");
    await userEvent.click(await within(dialog).findByRole("option", { name: /Ann Owner/ }));
    await waitFor(() => expect(sent.find((each) => each.method === "PUT")?.body).toEqual({ userId: ann.id }));
    expect(await within(dialog).findByText("Ann Owner")).toBeInTheDocument();
    expect(screen.getByText("Owner: Ann Owner")).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Remove the owner" }));
    expect(await within(dialog).findByText("Nobody owns this page yet.")).toBeInTheDocument();
    expect(sent.some((each) => each.method === "DELETE" && each.path === `/pages/${pageId}/owner`)).toBe(true);
    expect(await axeViolations()).toEqual([]);
  });

  it("shows the API's sentence when the owner may not view the page", async () => {
    stubPage(
      {},
      {
        "GET /people": { status: 200, body: { people: [{ id: ann.id, name: ann.name, email: "ann@example.test" }] } },
        [`PUT /pages/${pageId}/owner`]: {
          status: 422,
          body: { error: { code: "validation_failed", message: "Some fields need attention.", fields: { userId: "Ann Owner cannot view this page." } } },
        },
      },
    );
    await renderAt(PATH);
    const dialog = await openFromMenu();
    await userEvent.type(within(dialog).getByRole("combobox", { name: "Choose the owner" }), "ann");
    await userEvent.click(await within(dialog).findByRole("option", { name: /Ann Owner/ }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Ann Owner cannot view this page.");
  });

  it("verifies for the term chosen, verifies again and takes it away", async () => {
    let verification: Verification | null = null;
    const sent = stubPage(
      {},
      {
        [`PUT /pages/${pageId}/verification`]: async (request) => {
          const { days } = (await request.json()) as { days: number };
          verification = { ...verified, expiresAt: new Date(Date.parse(verified.verifiedAt) + days * 86_400_000).toISOString() };
          return { status: 200, body: { verification } };
        },
        [`DELETE /pages/${pageId}/verification`]: { status: 204 },
      },
    );
    await renderAt(PATH);
    const dialog = await openFromMenu();
    expect(within(dialog).getByText("Nobody has verified this page.")).toBeInTheDocument();
    const term = within(dialog).getByRole("combobox", { name: "Valid for" });
    expect(term).toHaveValue(String(VERIFY_DEFAULT_DAYS));
    expect(
      within(term)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(VERIFY_TERM_DAYS.map((n) => `${n} days`));
    await userEvent.selectOptions(term, "30");
    await userEvent.click(within(dialog).getByRole("button", { name: "Verify" }));
    await waitFor(() => expect(sent.find((each) => each.method === "PUT")?.body).toEqual({ days: 30 }));
    expect(await within(dialog).findByText(/^Verified by Ben Editor/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Verified until/ })).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Verify again" }));
    await waitFor(() => expect(sent.filter((each) => each.method === "PUT")).toHaveLength(2));
    await userEvent.click(within(dialog).getByRole("button", { name: "Remove the verification" }));
    expect(await within(dialog).findByText("Nobody has verified this page.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Verified until/ })).toBeNull();
  });
});
