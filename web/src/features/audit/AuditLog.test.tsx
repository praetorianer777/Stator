import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { auditExportHref, auditQuery, type AuditEntry } from "@/api/audit";
import { AUDIT_PATH } from "@/config";
import { renderAt, signedIn, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { describe as describeEntry, targetName } from "./AuditLog";

afterEach(() => vi.unstubAllGlobals());

const ada = signedIn.user.id;
const spaceId = "0199a000-0000-7000-8000-00000000005a";
const pageId = "0199a000-0000-7000-8000-0000000000aa";

const entry = (over: Partial<AuditEntry>): AuditEntry => ({
  id: crypto.randomUUID(),
  action: "space.created",
  targetType: "space",
  targetId: spaceId,
  actorId: ada,
  actorName: "Ada Lovelace",
  data: {},
  ip: "",
  createdAt: "2026-09-30T08:00:00Z",
  ...over,
});

const first = [
  entry({ action: "page.exported", targetType: "page", targetId: pageId, data: { title: "Plans", space: "DOC", scope: "subtree", pages: 3 }, ip: "10.0.0.7" }),
  entry({ action: "member.joined", targetType: "user", actorId: null, actorName: "", targetId: "0199a000-0000-7000-8000-0000000000b0" }),
  entry({ action: "token.revoked", targetType: "api_token", actorId: "0199a000-0000-7000-8000-0000000000c0", actorName: "", data: { owner: "x" } }),
  entry({ action: "space.created", data: { key: "DOC", name: "Docs" } }),
];
const second = [
  entry({ action: "sso.provider_saved", targetType: "oidc_provider", targetId: null, data: { issuer: "https://id.test", clientSecret: "kept" } }),
];

const facets = {
  facets: {
    actions: ["member.joined", "page.exported", "space.created", "sso.provider_saved", "token.revoked"],
    actors: [{ id: ada, name: "Ada Lovelace" }],
    targetTypes: ["page", "space", "user"],
    retentionDays: 365,
  },
};

/** The API's log, a page of four then a page of one, recording each query it was asked. */
function log(extra: Record<string, Answer | ((request: Request) => Answer)> = {}) {
  const queries: URLSearchParams[] = [];
  stubApi({
    "GET /audit/facets": { status: 200, body: facets },
    "GET /audit": (request) => {
      const query = new URL(request.url).searchParams;
      queries.push(query);
      return query.get("cursor") === "page-2"
        ? { status: 200, body: { entries: second, next: null } }
        : { status: 200, body: { entries: first, next: "page-2" } };
    },
    ...extra,
  });
  return queries;
}

describe("the audit log", () => {
  it("lists who did what to what, newest first, in words", async () => {
    log();
    await renderAt(AUDIT_PATH);
    const table = await screen.findByRole("table");
    const rows = within(table).getAllByRole("row");
    expect(rows).toHaveLength(first.length + 1);
    expect(rows[1]).toHaveTextContent("Page exported");
    expect(rows[1]).toHaveTextContent("page.exported");
    expect(rows[1]).toHaveTextContent("Plans");
    expect(rows[1]).toHaveTextContent("scope: subtree");
    expect(rows[1]).toHaveTextContent("from 10.0.0.7");
    expect(rows[2]).toHaveTextContent("System");
    expect(rows[3]).toHaveTextContent("Former member");
    expect(screen.getByText("Each entry is kept for 365 days.")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("narrows by action, person, kind and the reader's own days", async () => {
    const queries = log();
    await renderAt(AUDIT_PATH);
    await screen.findByRole("table");
    await userEvent.selectOptions(screen.getByLabelText("Action"), "space.created");
    await waitFor(() => expect(queries.at(-1)?.get("action")).toBe("space.created"));
    await userEvent.selectOptions(screen.getByLabelText("Person"), ada);
    await waitFor(() => expect(queries.at(-1)?.get("actor")).toBe(ada));
    await userEvent.selectOptions(screen.getByLabelText("Kind of target"), "space");
    await waitFor(() => expect(queries.at(-1)?.get("targetType")).toBe("space"));
    await userEvent.type(screen.getByLabelText("From"), "2026-09-01");
    await waitFor(() => expect(queries.at(-1)?.get("from")).toBe(new Date(2026, 8, 1).toISOString()));
    expect(screen.getByRole("link", { name: "Export CSV" }).getAttribute("href")).toContain("action=space.created");

    await userEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(screen.getByLabelText("Action")).toHaveValue("");
    expect(screen.getByLabelText("From")).toHaveValue("");
    expect(screen.getByRole("link", { name: "Export CSV" })).toHaveAttribute("href", "/api/v1/audit/export");
    expect(within(await screen.findByRole("table")).getAllByRole("row")).toHaveLength(first.length + 1);
  });

  it("shows only one target's entries when its name is followed, until told otherwise", async () => {
    const queries = log();
    await renderAt(AUDIT_PATH);
    await userEvent.click(await screen.findByRole("button", { name: "Show only entries about Plans" }));
    await waitFor(() => expect(queries.at(-1)?.get("target")).toBe(pageId));
    expect(screen.getByText("Only entries about Plans")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Export CSV" }).getAttribute("href")).toContain(`target=${pageId}`);
    await userEvent.click(screen.getByRole("button", { name: "Show every target" }));
    expect(screen.queryByText("Only entries about Plans")).toBeNull();
    expect(screen.getByRole("link", { name: "Export CSV" }).getAttribute("href")).not.toContain("target=");
  });

  it("pages older and back newer by the cursor the API handed out", async () => {
    const queries = log();
    await renderAt(AUDIT_PATH);
    await screen.findByRole("table");
    expect(screen.getByText("Page 1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Newer" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Older" }));
    await waitFor(() => expect(queries.at(-1)?.get("cursor")).toBe("page-2"));
    await waitFor(() => expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(second.length + 1));
    expect(within(screen.getByRole("table")).getByText("Identity provider saved")).toBeInTheDocument();
    expect(screen.getByText("Page 2")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Older" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Newer" }));
    expect(await screen.findByText("Page 1")).toBeInTheDocument();
  });

  it("puts a range the server refuses beside the field", async () => {
    log({
      "GET /audit": {
        status: 422,
        body: {
          error: { code: "validation_failed", message: "Some fields need attention.", fields: { to: "The end of the range has to come after its start." } },
        },
      },
    });
    await renderAt(AUDIT_PATH);
    expect(await screen.findByText("The end of the range has to come after its start.")).toBeInTheDocument();
    expect(screen.getByLabelText("To")).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("tells a member it is the administrators' to read", async () => {
    const sent = log();
    await renderAt(AUDIT_PATH, { me: { ...signedIn, organization: { ...signedIn.organization!, role: "member" } } });
    expect(await screen.findByText(/Only an administrator of this organization reads its audit log/)).toBeInTheDocument();
    expect(sent).toHaveLength(0);
  });
});

describe("an entry in words", () => {
  it("is known by the name it kept, else its id, and its details leave ids out", () => {
    expect(targetName(first[0]!)).toBe("Plans");
    expect(targetName(entry({ data: {} }))).toBe(spaceId);
    const details = describeEntry(entry({ data: { key: "DOC", authorId: "x", grants: [{ subject: "a" }, { subject: "b" }], scopes: ["read"] } }));
    expect(details).toBe("key: DOC · grants: 2 · scopes: read");
  });

  it("asks for the reader's days as the instants they span", () => {
    const query = auditQuery({ from: "2026-09-01", to: "2026-09-30" });
    expect(query.from).toBe(new Date(2026, 8, 1).toISOString());
    expect(query.to).toBe(new Date(2026, 9, 1).toISOString());
    expect(auditExportHref({})).toBe("/api/v1/audit/export");
    expect(auditExportHref({ action: "page.exported" })).toBe("/api/v1/audit/export?action=page.exported");
  });
});
