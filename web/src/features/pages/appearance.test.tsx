import { afterEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Attachment } from "@/api/attachments";
import type { TreeNode } from "@/api/tree";
import { arrival, renderAt, stubApi, type Answer } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { aPage, aSpace } from "@/test/spaces";
import { coverPosition, focusAt } from "./AppearanceDialog";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const space = aSpace();
const home = aPage();
const PAGE_ID = "0195f000-0000-7000-8000-0000000000a1";
const PICTURE = "0195f000-0000-7000-8000-0000000000f2";
const PATH = `/s/DOCS/p/${PAGE_ID}/plans`;
const plans = aPage({ id: PAGE_ID, title: "Plans", home: false, parentId: home.id, ancestors: [{ id: home.id, title: "Handbook", home: true }] });
const picture: Attachment = {
  id: PICTURE,
  pageId: PAGE_ID,
  fileName: "harbour.png",
  contentType: "image/png",
  size: 2048,
  width: 1600,
  height: 900,
  createdAt: "2026-09-01T10:00:00Z",
  uploadedByName: "Ada",
  version: 1,
  versions: 1,
  restoredFrom: null,
  preview: "none",
};
const node: TreeNode = {
  id: PAGE_ID,
  parentId: home.id,
  title: "Plans",
  hasChildren: false,
  unpublished: false,
  restricted: false,
  archived: false,
  kind: "page",
  icon: "🚀",
};

function stubPage(page = plans, more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [node] } },
    [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    [`GET /pages/${PAGE_ID}`]: { status: 200, body: { page, space } },
    [`GET /pages/${PAGE_ID}/attachments`]: { status: 200, body: { attachments: [picture] } },
    ...more,
  });
}

describe("a cover's focus", () => {
  it("is where the picture was clicked, in percent, and keeps that point in view", () => {
    expect(focusAt({ clientX: 150, clientY: 75 }, { left: 100, top: 50, width: 200, height: 100 })).toEqual({ focusX: 25, focusY: 25 });
    expect(focusAt({ clientX: 0, clientY: 999 }, { left: 100, top: 50, width: 200, height: 100 })).toEqual({ focusX: 0, focusY: 100 });
    expect(coverPosition({ focusX: 30, focusY: 70 })).toBe("30% 70%");
  });
});

describe("a page's appearance", () => {
  it("shows its cover cut to its focus, its emoji before the title and in the tree, and its width", async () => {
    stubPage(aPage({ ...plans, appearance: { icon: "🚀", width: "full", cover: { attachmentId: PICTURE, focusX: 30, focusY: 70 } } }));
    const router = await renderAt(PATH);
    await arrival(router, PATH);
    const article = document.querySelector(`[data-page="${PAGE_ID}"]`)!;
    expect(article).toHaveAttribute("data-page-width", "full");
    expect(article.querySelector("[data-page-cover] img")).toHaveStyle({ objectPosition: "30% 70%" });
    expect(document.querySelector("[data-page-icon]")).toHaveTextContent("🚀");
    await waitFor(() => expect(document.querySelector(`[data-tree-icon="🚀"]`)).not.toBeNull());
  });

  it("is chosen in a dialog: an emoji found by name, a width, a picture of the page and its focus", async () => {
    const sent: unknown[] = [];
    stubPage(plans, {
      [`PUT /pages/${PAGE_ID}/appearance`]: async (request) => {
        const body = await request.json();
        sent.push(body);
        return { status: 200, body: { appearance: body } };
      },
    });
    const user = userEvent.setup();
    const router = await renderAt(PATH);
    await arrival(router, PATH);
    await user.click(await screen.findByRole("button", { name: "Page actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Appearance" }));
    const dialog = await screen.findByRole("dialog", { name: "Page appearance" });
    await user.type(within(dialog).getByLabelText("Find an emoji"), "rocket");
    await user.click(await within(dialog).findByRole("button", { name: "Use rocket" }));
    await user.click(within(dialog).getByRole("radio", { name: /Full, text as wide as the window/ }));
    await user.click(await within(dialog).findByRole("radio", { name: "harbour.png" }));
    const focus = within(dialog).getByRole("button", { name: /Cover focus, 50% from the left/ });
    focus.focus();
    await user.keyboard("{ArrowRight}{ArrowUp}");
    expect(focus).toHaveAccessibleName("Cover focus, 55% from the left and 45% from the top");
    expect(await axeViolations({ popupOpen: true })).toEqual([]);
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(sent).toEqual([{ icon: "🚀", width: "full", cover: { attachmentId: PICTURE, focusX: 55, focusY: 45 } }]);
    expect(document.querySelector("[data-page-icon]")).toHaveTextContent("🚀");
  });
});
