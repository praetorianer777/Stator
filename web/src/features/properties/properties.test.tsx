import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { stubApi } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { allowlist, problems } from "@/test/allowlist";
import { PROPERTY_KEY_MAX_LENGTH } from "@/config";
import { DocView, drawInline } from "@/features/editor/DocView";
import { Editor } from "@/features/editor/Editor";
import type { Doc, DocNode } from "@/features/editor/schema";
import { PropertiesReport } from "./PropertiesReport";
import { PropertiesReportDialog, checkReport } from "./PropertiesReportDialog";
import { compareValues, reportSettings, type ReportSettings } from "./report";

afterEach(cleanup);

function find(doc: DocNode | undefined, type: string): DocNode[] {
  if (!doc) return [];
  const out: DocNode[] = doc.type === type ? [doc] : [];
  for (const child of doc.content ?? []) out.push(...find(child, type));
  return out;
}

function shown(children: ReactNode) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <main>
        <h1>Register</h1>
        {children}
      </main>
    </QueryClientProvider>,
  );
}

const text = (value: string, marks?: DocNode["marks"]): DocNode => ({ type: "text", text: value, ...(marks ? { marks } : {}) });
const withProperties: Doc = {
  type: "doc",
  content: [
    {
      type: "properties",
      content: [
        { type: "propertyRow", attrs: { key: "Owner" }, content: [{ type: "mention", attrs: { id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b", label: "Ada" } }] },
        { type: "propertyRow", attrs: { key: "Status" }, content: [text("Accepted", [{ type: "bold" }])] },
        { type: "propertyRow", attrs: { key: " " }, content: [text("half typed")] },
      ],
    },
  ],
};

describe("the properties block", () => {
  it("reads as a table of names and values, a row without a name left out", () => {
    const { container } = render(<DocView doc={withProperties} />);
    const rows = [...container.querySelectorAll("table[data-properties] tr")];
    expect(rows.map((r) => r.querySelector("th")?.textContent)).toEqual(["Owner", "Status"]);
    expect(rows[0]).toHaveTextContent("@Ada");
    expect(rows[1]?.querySelector("strong")).toHaveTextContent("Accepted");
    expect(allowlist.nodes.propertyRow?.attrs?.key?.maxLength).toBe(PROPERTY_KEY_MAX_LENGTH);
  });

  it("starts with the usual rows from the slash menu, takes a name in its field, and adds and removes rows from the keyboard", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <main>
        <h1>Register</h1>
        <Editor id="page-body" value={null} onChange={onChange} />
      </main>,
    );
    const box = document.getElementById("page-body")!;
    const last = () => onChange.mock.calls.at(-1)?.[0] as Doc;
    await user.click(box);
    await user.type(box, "/properties");
    await screen.findByRole("listbox", { name: "Insert a block" });
    await user.keyboard("{Enter}");
    await waitFor(() => expect(box.querySelectorAll("input[data-property-key]")).toHaveLength(2));
    expect(find(last(), "propertyRow").map((r) => r.attrs?.key)).toEqual(["Owner", "Status"]);

    // The caret is in the first value; Enter starts a row whose name comes first.
    await user.keyboard("Ada");
    await user.keyboard("{Enter}");
    const names = () => [...box.querySelectorAll<HTMLInputElement>("input[data-property-key]")];
    await waitFor(() => expect(names()).toHaveLength(3));
    await waitFor(() => expect(names()[1]).toHaveFocus());
    await user.keyboard("Review date{Enter}2026-11-01");
    await waitFor(() =>
      expect(find(last(), "propertyRow").map((r) => [r.attrs?.key, find(r, "text")[0]?.text])).toEqual([
        ["Owner", "Ada"],
        ["Review date", "2026-11-01"],
        ["Status", undefined],
      ]),
    );
    expect(screen.getAllByRole("textbox", { name: "Property name" })).toHaveLength(3);
    expect(problems(last())).toEqual([]);

    // Backspace in an empty value takes its row out.
    await user.keyboard("{Enter}");
    await waitFor(() => expect(names()).toHaveLength(4));
    // Enter in a name goes on to its value.
    await user.keyboard("{Enter}");
    await user.keyboard("{Backspace}");
    await waitFor(() => expect(find(last(), "propertyRow")).toHaveLength(3));
    expect(await axeViolations()).toEqual([]);
  });
});

describe("report settings", () => {
  it("put right what the server would refuse", () => {
    expect(reportSettings({ labels: ["ADR", "adr", "a/b", 4], space: "docs", columns: [" Owner ", "owner", "", "x".repeat(61)] })).toEqual({
      labels: ["adr"],
      space: null,
      columns: ["Owner"],
    });
    expect(reportSettings({ labels: ["adr"], space: "DOCS", columns: [] })).toEqual({ labels: ["adr"], space: "DOCS", columns: [] });
    expect(compareValues("10", "9", "en")).toBeGreaterThan(0);
    expect(compareValues(null, "a", "en")).toBeGreaterThan(0);
  });

  it("read labels and columns as typed, and say what is wrong", () => {
    expect(checkReport("ADR, Release Notes, adr", "", "Owner\n\n  Review   date\nowner")).toEqual({
      settings: { labels: ["adr", "release-notes"], space: null, columns: ["Owner", "Review date"] },
    });
    expect(checkReport(" , ", "DOCS", "")).toEqual({ problems: { labels: "Name at least one label to gather pages by." } });
    expect(checkReport("a/b", "", "")).toEqual({ problems: { labels: '"a/b" cannot be a label. Use letters, digits, hyphens, underscores and dots.' } });
    expect("problems" in checkReport("a,b,c,d,e,f", "", "")).toBe(true);
    expect("problems" in checkReport("a", "", "x".repeat(PROPERTY_KEY_MAX_LENGTH + 1))).toBe(true);
    expect("problems" in checkReport("a", "", "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk")).toBe(true);
  });
});

const settings: ReportSettings = { labels: ["adr"], space: null, columns: [] };
const row = (title: string, values: (string | null)[]) => ({
  pageId: `0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a${title.length}${title.charCodeAt(0)}`.slice(0, 36),
  title,
  spaceKey: "ADR",
  updatedAt: "2026-10-01T00:00:00Z",
  values: values.map((v) => (v === null ? null : { key: "x", text: v, content: [text(v, v === "Accepted" ? [{ type: "bold" }] : undefined)] })),
});

describe("the properties report", () => {
  it("tabulates each page's values, rich ones too, and sorts by a column", async () => {
    const asked: URLSearchParams[] = [];
    stubApi({
      "GET /properties-report": (request) => {
        asked.push(new URL(request.url).searchParams);
        return {
          status: 200,
          body: {
            columns: ["Status", "Review date"],
            rows: [row("Drop the cache", ["Proposed", "2026-11-01"]), row("No metadata", [null, null]), row("Use Postgres", ["Accepted", "2026-09-30"])],
            truncated: false,
          },
        };
      },
    });
    const user = userEvent.setup();
    shown(<PropertiesReport settings={{ labels: ["adr", "backend"], space: "ADR", columns: ["Status", "Review date"] }} draw={drawInline} inEditor />);
    const table = await screen.findByRole("table", { name: /3 pages labelled adr, backend/ });
    expect(String(asked[0])).toBe("label=adr&label=backend&space=ADR&column=Status&column=Review+date");
    const titles = () =>
      within(table)
        .getAllByRole("rowheader")
        .map((th) => th.textContent);
    expect(titles()).toEqual(["Drop the cache", "No metadata", "Use Postgres"]);
    expect(within(table).getByText("Accepted").tagName).toBe("STRONG");
    expect(screen.getByText("Pages labelled adr, backend in ADR")).toBeInTheDocument();

    await user.click(within(table).getByRole("button", { name: /Review date/ }));
    expect(titles()).toEqual(["Use Postgres", "Drop the cache", "No metadata"]);
    expect(within(table).getByRole("columnheader", { name: /Review date/ })).toHaveAttribute("aria-sort", "ascending");
    await user.click(within(table).getByRole("button", { name: /Review date/ }));
    expect(titles()).toEqual(["No metadata", "Drop the cache", "Use Postgres"]);
    expect(await axeViolations()).toEqual([]);
  });

  it("says when no page carries the labels, and when it stopped short", async () => {
    stubApi({ "GET /properties-report": { status: 200, body: { columns: [], rows: [], truncated: false } } });
    shown(<PropertiesReport settings={settings} draw={drawInline} inEditor />);
    expect(await screen.findByText("No published page you can read carries the label adr.")).toBeInTheDocument();
    cleanup();
    stubApi({ "GET /properties-report": { status: 200, body: { columns: [], rows: [row("One", [])], truncated: true } } });
    shown(<PropertiesReport settings={settings} draw={drawInline} inEditor />);
    expect(await screen.findByText(/Only the first 200 pages are listed/)).toBeInTheDocument();
  });

  it("is set up in a dialog that names the spaces", async () => {
    stubApi({ "GET /spaces": { status: 200, body: { spaces: [{ key: "ADR", name: "Decisions" }] } } });
    const user = userEvent.setup();
    const onSave = vi.fn();
    shown(<PropertiesReportDialog initial={{ labels: [], space: null, columns: [] }} isNew onSave={onSave} onClose={() => {}} />);
    const dialog = await screen.findByRole("dialog", { name: "Insert a properties report" });
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(within(dialog).getByText("Name at least one label to gather pages by.")).toBeInTheDocument();
    await user.type(within(dialog).getByLabelText("Labels"), "ADR");
    await within(dialog).findByRole("option", { name: "Decisions (ADR)" });
    await user.selectOptions(within(dialog).getByLabelText("Space"), "ADR");
    await user.type(within(dialog).getByLabelText("Properties to show"), "Status");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Insert" }));
    expect(onSave).toHaveBeenCalledWith({ labels: ["adr"], space: "ADR", columns: ["Status"] });
  });
});
