import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axeViolations } from "@/test/axe";
import { allowlist, attrProblem, problems } from "@/test/allowlist";
import { STATUS_LABEL_MAX_LENGTH } from "@/config";
import { DocView } from "./DocView";
import { Editor } from "./Editor";
import { loadEmoji, matchEmoji } from "./emoji";
import { formatDay, isoDay, statusLabel, today } from "./InlineValueViews";
import { STATUS_COLORS, type Doc, type DocNode } from "./schema";

afterEach(cleanup);

function find(doc: DocNode | undefined, type: string): DocNode[] {
  if (!doc) return [];
  const out: DocNode[] = doc.type === type ? [doc] : [];
  for (const child of doc.content ?? []) out.push(...find(child, type));
  return out;
}

function setup(value: Doc | null = null) {
  const user = userEvent.setup();
  const onChange = vi.fn();
  render(
    <main>
      <Editor id="page-body" value={value} onChange={onChange} />
    </main>,
  );
  const box = document.getElementById("page-body")!;
  const last = () => onChange.mock.calls.at(-1)?.[0] as Doc | undefined;
  return { user, box, last };
}

async function slash(user: ReturnType<typeof userEvent.setup>, box: HTMLElement, query: string) {
  await user.click(box);
  await user.type(box, `/${query}`);
  await screen.findByRole("listbox", { name: "Insert a block" });
  await user.keyboard("{Enter}");
}

const withValues: Doc = {
  type: "doc",
  content: [
    {
      type: "paragraph",
      content: [
        { type: "text", text: "Rollout " },
        { type: "status", attrs: { label: "In review", color: "warning" } },
        { type: "text", text: " until " },
        { type: "date", attrs: { date: "2026-11-02" } },
      ],
    },
  ],
};

describe("days", () => {
  it("are days that exist, written as the API takes them", () => {
    expect(isoDay("2026-11-02")).toBe("2026-11-02");
    expect(isoDay("2028-02-29")).toBe("2028-02-29");
    for (const bad of ["2027-02-29", "2026-13-01", "2026-02-30", "2026-1-5", "2026-11-02T00:00:00Z", "", null, 20261102]) {
      expect(isoDay(bad), String(bad)).toBeNull();
    }
  });

  it("are today in the author's calendar, and read the same in every time zone", () => {
    expect(today(new Date(2026, 0, 5, 23, 59))).toBe("2026-01-05");
    expect(formatDay("2026-11-02", "en-US")).toBe("Nov 2, 2026");
    expect(formatDay("2026-11-02", "de-DE")).toBe("02.11.2026");
    expect(formatDay("2026-01-01", "en-GB")).toBe("1 Jan 2026");
  });
});

describe("status labels", () => {
  it("offer exactly the colours and length the server takes", () => {
    const attrs = allowlist.nodes.status?.attrs;
    expect(attrs?.color?.enum).toEqual([...STATUS_COLORS]);
    expect(attrs?.label?.maxLength).toBe(STATUS_LABEL_MAX_LENGTH);
    expect(allowlist.nodes.date?.attrs?.date?.date).toBe(true);
    expect(attrProblem(allowlist.nodes.date!.attrs!.date!, "2027-02-29")).not.toBeNull();
    expect(attrProblem(allowlist.nodes.date!.attrs!.date!, today())).toBeNull();
  });

  it("keep some words, cut at the API's limit", () => {
    expect(statusLabel("   ")).toBeNull();
    expect(statusLabel(42)).toBeNull();
    expect(statusLabel("é".repeat(50))).toHaveLength(40);
  });
});

describe("emoji", () => {
  it("are found by shortcode first, then by tag and description, and the common ones come before a letter", async () => {
    const all = await loadEmoji();
    expect(all.length).toBeGreaterThan(1000);
    expect(matchEmoji(all, "tada")[0]?.emoji).toBe("🎉");
    expect(matchEmoji(all, "+1")[0]?.emoji).toBe("👍");
    expect(matchEmoji(all, "x")[0]?.names).toContain("x");
    expect(matchEmoji(all, "party").map((e) => e.emoji)).toContain("🎉");
    expect(matchEmoji(all, "")).toHaveLength(8);
    expect(matchEmoji(all, "").map((e) => e.emoji)).toEqual(["👍", "✅", "❌", "⚠️", "🎉", "🚀", "👀", "❤️"]);
    expect(matchEmoji(all, "zzzzqq")).toEqual([]);
    expect(matchEmoji(all, "s", 3)).toHaveLength(3);
  });

  it("go in as text from a colon, picked with the keyboard", async () => {
    const { user, box, last } = setup();
    await user.click(box);
    await user.type(box, "Shipped :tad");
    const list = await screen.findByRole("listbox", { name: "Emoji" });
    await waitFor(() => expect(within(list).getAllByRole("option")[0]).toHaveTextContent(":tada:"));
    expect(box).toHaveAttribute("aria-controls", list.id);
    expect(box.getAttribute("aria-activedescendant")).toBe(within(list).getAllByRole("option")[0]?.id);
    await user.keyboard("{Enter}");
    expect(
      find(last(), "text")
        .map((n) => n.text)
        .join(""),
    ).toBe("Shipped 🎉");
    expect(problems(last()!)).toEqual([]);
  });

  it("say when no emoji has that name", async () => {
    const { user, box } = setup();
    await user.click(box);
    await user.type(box, ":zzzzqq");
    await waitFor(() => expect(document.querySelector("[data-emoji-empty]")).toHaveTextContent("No emoji by that name"));
    expect(document.querySelector("[data-emoji-empty]")).toHaveAttribute("role", "status");
    expect(screen.queryByRole("listbox", { name: "Emoji" })).toBeNull();
  });

  it("open from the slash menu as from a colon", async () => {
    const { user, box } = setup();
    await slash(user, box, "emoji");
    const list = await screen.findByRole("listbox", { name: "Emoji" });
    await waitFor(() => expect(within(list).getAllByRole("option")).toHaveLength(8));
  });

  it("are offered in a comment too", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Editor id="comment-body" value={null} onChange={onChange} variant="comment" />);
    const box = document.getElementById("comment-body")!;
    await user.click(box);
    await user.type(box, ":rocke");
    await screen.findByRole("listbox", { name: "Emoji" });
    await user.keyboard("{Enter}");
    expect(JSON.stringify(onChange.mock.calls.at(-1)?.[0])).toContain("🚀");
  });
});

describe("the status dialog", () => {
  it("opens on a new status from the slash menu and saves its words and colour", async () => {
    const { user, box, last } = setup();
    await slash(user, box, "status");
    const dialog = await screen.findByRole("dialog", { name: "Status" });
    const words = within(dialog).getByLabelText("Words");
    expect(words).toHaveFocus();
    expect(words).toHaveValue("To do");
    await user.clear(words);
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(within(dialog).getByText(/Write a word or two for the status/)).toBeInTheDocument();
    expect(words).toHaveAttribute("aria-invalid", "true");
    await user.type(words, "Blocked");
    const red = within(dialog).getByRole("radio", { name: "Red" });
    await user.click(red);
    expect(red).toBeChecked();
    expect(within(dialog).getByText("Blocked", { selector: "[data-status-preview] .doc-status" })).toHaveAttribute("data-status-label", "danger");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.keyboard("{Enter}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(find(last(), "status")[0]?.attrs).toEqual({ label: "Blocked", color: "danger" });
    expect(problems(last()!)).toEqual([]);
    expect(box.querySelector(".doc-status")).toHaveTextContent("Status Blocked");
  });

  it("opens on a click and leaves the status as it was on Escape", async () => {
    const { user, box } = setup(withValues);
    // A node view is drawn once React gets to it, after the editor itself.
    await waitFor(() => expect(box.querySelector(".doc-status")).not.toBeNull());
    await user.click(box.querySelector(".doc-status")!);
    const dialog = await screen.findByRole("dialog", { name: "Status" });
    expect(within(dialog).getByLabelText("Words")).toHaveValue("In review");
    expect(within(dialog).getByRole("radio", { name: "Yellow" })).toBeChecked();
    await user.type(within(dialog).getByLabelText("Words"), " again");
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(box.querySelector(".doc-status")).toHaveTextContent("Status In review");
  });
});

describe("the date dialog", () => {
  it("opens on today's date from the slash menu and saves the day picked", async () => {
    const { user, box, last } = setup();
    await slash(user, box, "date");
    const dialog = await screen.findByRole("dialog", { name: "Date" });
    const day = within(dialog).getByLabelText("Day");
    expect(day).toHaveValue(today());
    expect(day).toHaveFocus();
    await user.clear(day);
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(within(dialog).getByText(/Pick a day/)).toBeInTheDocument();
    await user.type(day, "2026-11-02");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(find(last(), "date")[0]?.attrs).toEqual({ date: "2026-11-02" });
    expect(problems(last()!)).toEqual([]);
    expect(box.querySelector("time")).toHaveAttribute("datetime", "2026-11-02");
    expect(box.querySelector("time")).toHaveTextContent(formatDay("2026-11-02"));
  });
});

describe("the read-only view", () => {
  it("draws a status as its words with its colour, and a date as a time in the reader's format", async () => {
    render(
      <main>
        <h1>Rollout plan</h1>
        <DocView doc={withValues} />
      </main>,
    );
    const status = document.querySelector("[data-doc] .doc-status")!;
    expect(status).toHaveTextContent("Status In review");
    expect(status).toHaveAttribute("data-status-label", "warning");
    const time = document.querySelector("[data-doc] time")!;
    expect(time).toHaveAttribute("datetime", "2026-11-02");
    expect(time).toHaveTextContent(formatDay("2026-11-02"));
    expect(document.querySelector("[data-doc] p")).toHaveTextContent(`Rollout Status In review until ${formatDay("2026-11-02")}`);
    expect(await axeViolations()).toEqual([]);
  });

  it("leaves out a status with no words, a day that does not exist, and an unknown colour's tint", () => {
    render(
      <DocView
        doc={{
          type: "doc",
          content: [
            {
              type: "paragraph",
              content: [
                { type: "status", attrs: { label: " ", color: "danger" } },
                { type: "date", attrs: { date: "2026-02-30" } },
                { type: "status", attrs: { label: "Odd", color: "#f00" } },
              ],
            },
          ],
        }}
      />,
    );
    expect(document.querySelectorAll(".doc-status")).toHaveLength(1);
    expect(document.querySelector(".doc-status")).toHaveAttribute("data-status-label", "neutral");
    expect(document.querySelector("time")).toBeNull();
  });
});
