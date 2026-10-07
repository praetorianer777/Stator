import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CollabSession } from "@/features/collab/session";
import { setSharedTitle, titleOf } from "@/features/collab/shared";
import { collabTransport } from "@/features/collab/transport";
import { fillSharedDraft } from "@/features/editor/sharedDraft";
import { renderAt, stubApi, type Answer } from "@/test/app";
import { FakeCollabServer } from "@/test/collabServer";
import { aPage, aSpace } from "@/test/spaces";

// The editor's chunk loads and the editor mounts in jsdom, which is slow on a busy machine.
const EDITOR_TEST_MS = 20_000;

const space = aSpace();
const home = aPage();
const PAGE_ID = "0195f000-0000-7000-8000-0000000000b2";
const plans = aPage({
  id: PAGE_ID,
  title: "Plans",
  home: false,
  parentId: home.id,
  ancestors: [{ id: home.id, title: "Handbook", home: true }],
  version: 3,
});
const EDIT_PATH = `/s/DOCS/p/${PAGE_ID}/plans/edit`;
const paragraph = (text: string) => ({ type: "paragraph", content: [{ type: "text", text }] });

function stubPage(more: Record<string, Answer | ((request: Request) => Answer | Promise<Answer>)> = {}) {
  return stubApi({
    "GET /spaces": { status: 200, body: { spaces: [space] } },
    "GET /spaces/DOCS": { status: 200, body: { space } },
    "GET /spaces/DOCS/pages": { status: 200, body: { pages: [] } },
    [`GET /pages/${home.id}`]: { status: 200, body: { page: home, space } },
    [`GET /pages/${PAGE_ID}`]: { status: 200, body: { page: plans, space } },
    [`GET /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft: null } },
    ...more,
  });
}

let server: FakeCollabServer;
let bob: CollabSession;
const was = { ...collabTransport };

beforeEach(async () => {
  server = new FakeCollabServer(3);
  collabTransport.enabled = true;
  collabTransport.socket = server.socket;
  // Bob was there first, and seeded the room with his own words.
  bob = new CollabSession({
    url: "ws://test",
    user: { id: "u-bob", name: "Bob Builder", color: "#b45309" },
    seed: async () => ({ base: 3, fill: (doc) => fillSharedDraft(doc, "Shared plans", { type: "doc", content: [paragraph("Written by Bob.")] }) }),
    socket: server.socket,
    local: null,
  });
  await vi.waitFor(() => expect(bob.state.ready).toBe(true));
});

afterEach(() => {
  bob.destroy();
  Object.assign(collabTransport, was);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("editing a page together", () => {
  it(
    "opens the shared draft, shows who else is in it, and follows their changes",
    async () => {
      stubPage();
      await renderAt(EDIT_PATH);
      const title = await screen.findByLabelText("Title");
      expect(document.querySelector("[data-collab]")).toHaveAttribute("data-collab", "together");
      expect(title).toHaveValue("Shared plans");
      await waitFor(() => expect(document.getElementById("page-body")).toHaveTextContent("Written by Bob."));
      const present = await screen.findByRole("list", { name: "Editing now: Bob Builder" });
      expect(within(present).getByText("Bob Builder")).toBeInTheDocument();

      setSharedTitle(titleOf(bob.state.doc!), "Shared plans for 2027");
      await waitFor(() => expect(title).toHaveValue("Shared plans for 2027"));
      await userEvent.type(title, "!");
      await vi.waitFor(() => expect(titleOf(bob.state.doc!).toString()).toBe("Shared plans for 2027!"));
    },
    EDITOR_TEST_MS,
  );

  it(
    "publishes the shared draft as it stands, and tells the others",
    async () => {
      const published = aPage({ ...plans, title: "Shared plans", version: 4, draft: null });
      const sent = stubPage({
        [`PUT /pages/${PAGE_ID}/draft`]: { status: 200, body: { draft: { pageId: PAGE_ID, title: "Shared plans", body: {}, baseVersion: 3, updatedAt: "" } } },
        [`POST /pages/${PAGE_ID}/publish`]: { status: 200, body: { page: published, version: {} } },
      });
      const router = await renderAt(EDIT_PATH);
      await screen.findByLabelText("Title");
      await waitFor(() => expect(document.getElementById("page-body")).toHaveTextContent("Written by Bob."));
      await userEvent.click(screen.getByRole("button", { name: "Publish" }));
      const dialog = await screen.findByRole("dialog", { name: "Publish Shared plans" });
      await userEvent.click(within(dialog).getByRole("button", { name: "Publish" }));
      await waitFor(() => expect(router.state.location.pathname).toBe(`/s/DOCS/p/${PAGE_ID}/shared-plans`));
      const save = sent.find((r) => r.method === "PUT" && r.path === `/pages/${PAGE_ID}/draft`);
      expect(save?.body).toMatchObject({ title: "Shared plans", baseVersion: 3, body: { type: "doc", content: [paragraph("Written by Bob.")] } });
      await vi.waitFor(() => expect(bob.state.base).toBe(4));
      expect(server.published).toEqual([4]);
    },
    EDITOR_TEST_MS,
  );

  it(
    "edits alone when the shared draft is out of reach",
    async () => {
      collabTransport.enabled = false;
      stubPage();
      await renderAt(EDIT_PATH);
      expect(await screen.findByLabelText("Title")).toHaveValue("Plans");
      expect(document.querySelector("[data-collab]")).toHaveAttribute("data-collab", "alone");
    },
    EDITOR_TEST_MS,
  );
});
