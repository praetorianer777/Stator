import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Page } from "@/api/pages";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const home = aPage();
const pageId = "0195f000-0000-7000-8000-0000000000d1";
const PATH = `/s/DOCS/p/${pageId}/runbook`;
const ann = { id: "0195f000-0000-7000-8000-0000000000a1", name: "Ann Archer", email: "ann@stator.test", canView: true };
const carl = { id: "0195f000-0000-7000-8000-0000000000a3", name: "Carl Closed", email: "carl@stator.test", canView: false };
const team = { id: "0195f000-0000-7000-8000-0000000000b1", name: "Team", memberCount: 3, fromProvider: false, viewers: 2 };
const outsiders = { id: "0195f000-0000-7000-8000-0000000000b2", name: "Outsiders", memberCount: 1, fromProvider: false, viewers: 0 };

function stubPage({ page = {}, more = {} }: { page?: Partial<Page>; more?: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> } = {}) {
  const space = aSpace();
  const shown = aPage({
    id: pageId,
    title: "Runbook",
    home: false,
    parentId: home.id,
    ancestors: [{ id: home.id, title: "Handbook", home: true }],
    ...page,
  });
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${pageId}`]: { status: 200, body: { page: shown, space } },
    [`GET /pages/${pageId}/attachments`]: { status: 200, body: { attachments: [] } },
    [`POST /pages/${pageId}/visit`]: { status: 204 },
    [`GET /pages/${pageId}/viewers`]: {
      status: 200,
      body: { viewers: [{ id: ann.id, name: ann.name, email: ann.email }], total: 12, everyone: false, limit: 8, offset: 0 },
    },
    [`GET /pages/${pageId}/share/recipients`]: { status: 200, body: { people: [ann, carl], groups: [team, outsiders] } },
    ...more,
  });
}

async function openDialog() {
  await userEvent.click(await screen.findByRole("button", { name: "Share" }));
  return screen.findByRole("dialog", { name: "Share Runbook" });
}

async function pick(dialog: HTMLElement, name: string) {
  const box = within(dialog).getByRole("combobox", { name: "Send to" });
  await userEvent.clear(box);
  await userEvent.type(box, name.slice(0, 2));
  await userEvent.click(await within(dialog).findByRole("option", { name: new RegExp(`^${name}`) }));
}

describe("the share dialog", () => {
  it("says who can view the page and marks who may not, before anything is sent", async () => {
    stubPage({ page: { restricted: { view: true, edit: false } } });
    await renderAt(PATH);
    const dialog = await openDialog();
    expect(await within(dialog).findByText("This page is restricted. 12 people can view it:")).toBeInTheDocument();
    expect(dialog.querySelector('[data-viewer="Ann Archer"]')).not.toBeNull();
    expect(within(dialog).getByText("and 11 more")).toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole("combobox", { name: "Send to" }));
    const carlOption = await within(dialog).findByRole("option", { name: /^Carl Closed/ });
    expect(carlOption).toHaveTextContent("Cannot view this page");
    expect(within(dialog).getByRole("option", { name: /^Team/ })).toHaveTextContent("2 of 3 members can view this page; only they are told");
    expect(within(dialog).getByRole("option", { name: /^Outsiders/ })).toHaveTextContent("Nobody in it can view this page");
    await userEvent.click(carlOption);
    const picked = within(dialog).getByRole("list", { name: "Sending to" });
    expect(within(picked).getByText("Cannot view this page")).toBeInTheDocument();
    expect(within(dialog).getByText(/^Sharing does not give access\./)).toBeInTheDocument();
  });

  it("sends the people and groups picked with the note, and says how many were told", async () => {
    const sent = stubPage({
      more: {
        [`POST /pages/${pageId}/share`]: {
          status: 201,
          body: { share: { id: "s1", pageId, recipients: [], people: 3, message: "Read the rollback part.", createdAt: "2026-10-01T08:00:00Z" } },
        },
      },
    });
    await renderAt(PATH);
    const dialog = await openDialog();
    expect(within(dialog).getByRole("button", { name: "Share" })).toBeDisabled();
    await pick(dialog, "Ann Archer");
    await pick(dialog, "Team");
    await userEvent.type(within(dialog).getByRole("textbox", { name: "Note (optional)" }), "  Read the rollback part.  ");
    await userEvent.click(within(dialog).getByRole("button", { name: "Share" }));
    expect(await within(dialog).findByRole("status")).toHaveTextContent("Shared with 3 people.");
    await waitFor(() =>
      expect(sent.find((each) => each.method === "POST" && each.path === `/pages/${pageId}/share`)?.body).toEqual({
        recipients: [
          { type: "user", id: ann.id },
          { type: "group", id: team.id },
        ],
        message: "Read the rollback part.",
      }),
    );
    await userEvent.click(within(dialog).getByRole("button", { name: "Done" }));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("says nothing was shared when the page is closed to somebody, in the reader's words", async () => {
    stubPage({
      more: {
        [`POST /pages/${pageId}/share`]: {
          status: 409,
          body: { error: { code: "cannot_view", message: "This page is closed to Carl Closed, so nothing was shared." } },
        },
      },
    });
    await renderAt(PATH);
    const dialog = await openDialog();
    await pick(dialog, "Carl Closed");
    await userEvent.click(within(dialog).getByRole("button", { name: "Share" }));
    expect(await within(dialog).findByText(/^Nothing was shared, because the page is closed to somebody you picked\./)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Remove Carl Closed" }));
    expect(within(dialog).queryByText(/^Nothing was shared/)).toBeNull();
    expect(within(dialog).getByText("Nobody picked yet.")).toBeInTheDocument();
  });

  it("says when everybody can view the page, and passes axe", async () => {
    stubPage({
      more: {
        [`GET /pages/${pageId}/viewers`]: { status: 200, body: { viewers: [], total: 40, everyone: true, limit: 8, offset: 0 } },
      },
    });
    await renderAt(PATH);
    const dialog = await openDialog();
    expect(await within(dialog).findByText("Everyone in the organization can view this page.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("is not offered on a page nobody else can read yet", async () => {
    stubPage({ page: { unpublished: true, version: 0 } });
    await renderAt(PATH);
    expect(await screen.findByRole("button", { name: "History" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Share" })).toBeNull();
  });
});
