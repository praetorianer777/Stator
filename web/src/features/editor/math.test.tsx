import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axeViolations } from "@/test/axe";
import { allowlist, problems } from "@/test/allowlist";
import { MATH_DEFAULT_LATEX, MATH_MAX_LENGTH } from "@/config";
import { DocView } from "./DocView";
import { Editor } from "./Editor";
import { mathSource, typeset } from "./MathViews";
import type { Doc, DocNode } from "./schema";

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

const withFormulas: Doc = {
  type: "doc",
  content: [
    {
      type: "paragraph",
      content: [
        { type: "text", text: "Energy is " },
        { type: "mathInline", attrs: { latex: "E = mc^2" } },
      ],
    },
    { type: "mathBlock", attrs: { latex: "\\int_0^1 x\\,dx" } },
  ],
};

describe("typesetting", () => {
  it("draws TeX as KaTeX markup with MathML for screen readers, and says why it cannot", () => {
    const inline = typeset("\\frac{a}{b}", false);
    expect(inline.error).toBeNull();
    expect(inline.html).toContain('class="katex"');
    expect(inline.html).toContain("<math");
    expect(typeset("x", true).html).toContain("katex-display");
    const broken = typeset("\\frac{a", false);
    expect(broken.html).toBeNull();
    expect(broken.error).toMatch(/expected '}'/i);
  });

  it("draws no link, class or markup the author asks for", () => {
    for (const hostile of [
      "\\href{javascript:alert(1)}{x}",
      "\\url{https://example.test}",
      "\\htmlClass{evil}{x}",
      "\\htmlData{x=1}{y}",
      "\\includegraphics{https://example.test/a.png}",
    ]) {
      const out = typeset(hostile, false);
      expect(out.html ?? "", hostile).not.toMatch(/<a |<img |href=|class="evil"|data-x=/);
    }
  });

  it("stops a macro that calls itself rather than holding the page", () => {
    expect(typeset("\\def\\a{\\a\\a}\\a", false).error).toMatch(/Too many expansions/);
  });

  it("keeps a source the server takes, cut at its limit", () => {
    expect(mathSource("  ")).toBeNull();
    expect(mathSource(42)).toBeNull();
    expect(mathSource("x".repeat(MATH_MAX_LENGTH + 5))).toHaveLength(MATH_MAX_LENGTH);
    expect(allowlist.nodes.mathInline?.attrs?.latex?.maxLength).toBe(MATH_MAX_LENGTH);
    expect(allowlist.nodes.mathBlock?.attrs?.latex?.maxLength).toBe(MATH_MAX_LENGTH);
  });
});

describe("the read-only view", () => {
  it("typesets formulas in the line and on their own", () => {
    const { container } = render(<DocView doc={withFormulas} />);
    expect(container.querySelector('[data-math="inline"] .katex')).not.toBeNull();
    expect(container.querySelector('[data-math="block"] .katex-display')).not.toBeNull();
    expect(container.querySelector("p")).toHaveTextContent(/Energy is/);
  });

  it("shows the source of a formula it cannot typeset, and says so", () => {
    const { container } = render(<DocView doc={{ type: "doc", content: [{ type: "mathBlock", attrs: { latex: "\\frac{a" } }] }} />);
    const shown = container.querySelector("[data-math-error]");
    expect(shown).toHaveTextContent("This formula cannot be typeset; its source: \\frac{a");
    expect(shown?.getAttribute("title")).toMatch(/expected '}'/i);
  });
});

describe("the formula dialog", () => {
  it("opens on a new block formula from the slash menu, refuses what cannot be typeset and saves the rest", async () => {
    const { user, box, last } = setup();
    await slash(user, box, "formula");
    const dialog = await screen.findByRole("dialog", { name: "Formula" });
    const source = within(dialog).getByLabelText("LaTeX source");
    expect(source).toHaveValue(MATH_DEFAULT_LATEX);
    expect(source).toHaveFocus();
    await user.clear(source);
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(within(dialog).getByText(/Write the formula in LaTeX/)).toBeInTheDocument();
    await user.type(source, "\\frac{{a");
    expect(within(dialog).getByText(/This formula cannot be typeset: .*Change the source and save again\./)).toBeInTheDocument();
    expect(source).toHaveAttribute("aria-invalid", "true");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(screen.getByRole("dialog", { name: "Formula" })).toBeInTheDocument();
    await user.type(source, "}{{b}");
    expect(dialog.querySelector("[data-math-preview] .katex-display")).not.toBeNull();
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(find(last(), "mathBlock")[0]?.attrs).toEqual({ latex: "\\frac{a}{b}" });
    expect(problems(last()!)).toEqual([]);
    expect(box.querySelector('[data-math-edit="block"] .katex-display')).not.toBeNull();
  });

  it("puts an inline formula in the line from the slash menu", async () => {
    const { user, box, last } = setup();
    await user.click(box);
    await user.type(box, "Area ");
    await slash(user, box, "inline");
    const dialog = await screen.findByRole("dialog", { name: "Formula" });
    const source = within(dialog).getByLabelText("LaTeX source");
    await user.clear(source);
    await user.type(source, "\\pi r^2");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const line = find(last(), "paragraph")[0];
    expect(line?.content?.map((n) => n.type)).toEqual(["text", "mathInline"]);
    expect(line?.content?.[1]?.attrs).toEqual({ latex: "\\pi r^2" });
    expect(problems(last()!)).toEqual([]);
  });

  it("opens on a click with the formula's source, and leaves it as it was on Escape", async () => {
    const { user, box } = setup(withFormulas);
    // A node view is drawn once React gets to it, after the editor itself.
    await waitFor(() => expect(box.querySelector('[data-math-edit="inline"]')).not.toBeNull());
    await user.click(box.querySelector('[data-math-edit="inline"]')!);
    const dialog = await screen.findByRole("dialog", { name: "Formula" });
    expect(within(dialog).getByLabelText("LaTeX source")).toHaveValue("E = mc^2");
    await user.type(within(dialog).getByLabelText("LaTeX source"), "+1");
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    await user.click(box.querySelector('[data-math-edit="block"]')!);
    const again = await screen.findByRole("dialog", { name: "Formula" });
    expect(within(again).getByLabelText("LaTeX source")).toHaveValue("\\int_0^1 x\\,dx");
  });
});
