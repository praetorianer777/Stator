import type { Page } from "@/api/pages";
import type { Space } from "@/api/spaces";

/** A space the tests share, which an administrator may do anything in. */
export function aSpace(over: Partial<Space> = {}): Space {
  return {
    id: "0195f000-0000-7000-8000-00000000d0c5",
    key: "DOCS",
    name: "Handbook",
    description: "How we work.",
    homePageId: "0195f000-0000-7000-8000-000000000001",
    createdAt: "2026-09-29T08:00:00Z",
    updatedAt: "2026-09-29T08:00:00Z",
    archivedAt: null,
    archivedByName: "",
    can: { editPages: true, administer: true, delete: true, purgeTrash: true, addComments: true, deletePages: true },
    watching: false,
    starred: false,
    ...over,
  };
}

/** A page with a line of text, by default the home page of aSpace. */
export function aPage(over: Partial<Page> = {}): Page {
  return {
    id: "0195f000-0000-7000-8000-000000000001",
    spaceId: "0195f000-0000-7000-8000-00000000d0c5",
    spaceKey: "DOCS",
    parentId: null,
    title: "Handbook",
    kind: "page",
    body: { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "Welcome to the handbook." }] }] },
    version: 1,
    home: true,
    unpublished: false,
    draft: null,
    restricted: { view: false, edit: false },
    can: { edit: true, delete: true, restrict: true, comment: true, archive: true },
    ancestors: [],
    labels: [],
    comments: { page: 0, inline: 0, detached: 0 },
    watching: { page: false, subtree: false, inherited: null },
    starred: false,
    reactions: [],
    owner: null,
    verification: null,
    archived: null,
    createdByName: "Ada Lovelace",
    createdAt: "2026-09-29T08:00:00Z",
    updatedByName: "Ada Lovelace",
    updatedAt: "2026-09-29T08:00:00Z",
    ...over,
  };
}
