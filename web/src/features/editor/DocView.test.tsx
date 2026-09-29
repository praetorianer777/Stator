import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DocView } from "./DocView";
import type { Doc } from "./schema";

const doc: Doc = {
  type: "doc",
  content: [
    { type: "heading", attrs: { level: 1, id: "plan" }, content: [{ type: "text", text: "Plan" }] },
    {
      type: "paragraph",
      content: [
        { type: "text", text: "bold", marks: [{ type: "bold" }] },
        { type: "text", text: " and " },
        { type: "mention", attrs: { id: "u1", label: "Ada" } },
        { type: "hardBreak" },
        { type: "text", text: "next line" },
      ],
    },
    { type: "bulletList", content: [{ type: "listItem", content: [{ type: "paragraph", content: [{ type: "text", text: "item" }] }] }] },
    { type: "orderedList", attrs: { start: 3 }, content: [{ type: "listItem", content: [{ type: "paragraph", content: [{ type: "text", text: "third" }] }] }] },
    {
      type: "taskList",
      content: [{ type: "taskItem", attrs: { checked: true }, content: [{ type: "paragraph", content: [{ type: "text", text: "done" }] }] }],
    },
    { type: "codeBlock", attrs: { language: "go" }, content: [{ type: "text", text: "func main() {}" }] },
    { type: "horizontalRule" },
    {
      type: "table",
      content: [
        {
          type: "tableRow",
          content: [
            { type: "tableHeader", attrs: { colspan: 2, background: "accent" }, content: [{ type: "paragraph", content: [{ type: "text", text: "Head" }] }] },
          ],
        },
        {
          type: "tableRow",
          content: [
            { type: "tableCell", attrs: { background: "#f00" }, content: [{ type: "paragraph", content: [{ type: "text", text: "a" }] }] },
            { type: "tableCell", attrs: { align: "center" }, content: [{ type: "paragraph", content: [{ type: "text", text: "b" }] }] },
          ],
        },
      ],
    },
    { type: "panel", attrs: { kind: "warning" }, content: [{ type: "paragraph", content: [{ type: "text", text: "Careful" }] }] },
    {
      type: "paragraph",
      content: [
        { type: "text", text: "safe", marks: [{ type: "link", attrs: { href: "https://example.test" } }] },
        { type: "text", text: "local", marks: [{ type: "link", attrs: { href: "/spaces" } }] },
        { type: "text", text: "unsafe", marks: [{ type: "link", attrs: { href: "javascript:alert(1)" } }] },
      ],
    },
    { type: "iframe", content: [{ type: "text", text: "unknown" }] },
    { type: "heading", attrs: { level: 2, id: 'x" onmouseover="alert(1)' }, content: [{ type: "text", text: "Bad anchor" }] },
  ],
};

describe("DocView", () => {
  it("draws each node as the element that means it", () => {
    const { container } = render(<DocView doc={doc} />);
    expect(container.querySelector("h2#plan")?.textContent).toBe("Plan");
    expect(container.querySelector("strong")?.textContent).toBe("bold");
    expect(container.querySelector('[data-mention="u1"]')?.textContent).toBe("@Ada");
    expect(container.querySelector("br")).not.toBeNull();
    expect(container.querySelector("ul li")?.textContent).toBe("item");
    expect(container.querySelector("ol")?.getAttribute("start")).toBe("3");
    expect(screen.getByRole("checkbox", { name: "Done" })).toBeChecked();
    expect(container.querySelector("hr")).not.toBeNull();
    expect(container.querySelector("th")?.getAttribute("colspan")).toBe("2");
    expect(container.querySelector("th")?.getAttribute("data-background")).toBe("accent");
    expect(container.querySelector("td")?.hasAttribute("data-background")).toBe(false);
    expect((container.querySelectorAll("td")[1] as HTMLElement).style.textAlign).toBe("center");
    expect(screen.getByRole("note", { name: "Warning" })).toHaveTextContent("Careful");
    expect(container.textContent).toContain("unknown");
  });

  it("highlights code in its language without parsing it as markup", () => {
    const { container } = render(<DocView doc={doc} />);
    const code = container.querySelector("pre code.language-go")!;
    expect(code.textContent).toBe("func main() {}");
    expect(code.querySelector(".hljs-keyword")?.textContent).toBe("func");
    expect(container.querySelector("pre")).toHaveTextContent("Go");
  });

  // The one rule that keeps a document from being an injection: no HTML, ever.
  it("never turns an unsafe link into an anchor and never sets HTML", () => {
    const { container } = render(<DocView doc={doc} />);
    const anchors = Array.from(container.querySelectorAll("a"));
    expect(anchors.map((a) => a.getAttribute("href"))).toEqual(["https://example.test", "/spaces"]);
    expect(anchors[0]?.getAttribute("rel")).toBe("noopener noreferrer nofollow");
    expect(anchors[1]?.hasAttribute("target")).toBe(false);
    expect(container.textContent).toContain("unsafe");
    expect(container.querySelector("[onmouseover]")).toBeNull();
    expect(screen.getByText("Bad anchor").hasAttribute("id")).toBe(false);
    const scripted: Doc = { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "<img src=x onerror=alert(1)>" }] }] };
    const { container: other } = render(<DocView doc={scripted} />);
    expect(other.querySelector("img")).toBeNull();
    expect(other.textContent).toContain("<img src=x onerror=alert(1)>");
  });

  it("copies the link to a heading", async () => {
    const user = userEvent.setup();
    render(<DocView doc={doc} />);
    expect(screen.queryByRole("button", { name: "Copy link to Bad anchor" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Copy link to Plan" }));
    expect(await navigator.clipboard.readText()).toBe(`${window.location.origin}${window.location.pathname}#plan`);
    expect(screen.getByRole("status")).toHaveTextContent("Link copied");
  });
});
