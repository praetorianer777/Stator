import { afterEach, describe, expect, it, vi } from "vitest";
import * as Y from "yjs";
import { FakeCollabServer } from "@/test/collabServer";
import { CLOSE } from "./protocol";
import { CollabSession, type CollabUser, type SeedContent } from "./session";
import { titleOf } from "./shared";

const sessions: CollabSession[] = [];

afterEach(() => {
  for (const s of sessions.splice(0)) s.destroy();
  vi.useRealTimers();
});

const ann: CollabUser = { id: "ann", name: "Ann", color: "#1d4ed8" };
const bob: CollabUser = { id: "bob", name: "Bob", color: "#b45309" };

/** Seeds the shared title with words, as a page's editor seeds its body and title. */
const seedWith = (words: string, base = 1) => vi.fn(async (_afresh: boolean): Promise<SeedContent> => ({ base, fill: (doc) => titleOf(doc).insert(0, words) }));

function open(server: FakeCollabServer, user: CollabUser, seed = seedWith("Plans"), online = () => true) {
  const s = new CollabSession({ url: "ws://test/collab", user, seed, socket: server.socket, local: null, online });
  sessions.push(s);
  return s;
}

const titleIn = (s: CollabSession) => (s.state.doc ? titleOf(s.state.doc).toString() : null);

describe("a shared draft in the browser", () => {
  it("is seeded by the first in, and loaded by everybody after", async () => {
    const server = new FakeCollabServer(3);
    const annSeed = seedWith("Plans", 3);
    const a = open(server, ann, annSeed);
    await vi.waitFor(() => expect(a.state.ready).toBe(true));
    expect(annSeed).toHaveBeenCalledWith(false);
    expect(titleIn(a)).toBe("Plans");
    expect(a.state.base).toBe(3);

    const bobSeed = seedWith("Not this");
    const b = open(server, bob, bobSeed);
    await vi.waitFor(() => expect(b.state.ready).toBe(true));
    expect(titleIn(b)).toBe("Plans");
    expect(bobSeed).not.toHaveBeenCalled();
    expect(titleOf(server.doc()).toString()).toBe("Plans");
  });

  it("passes each change to the others as it is made", async () => {
    const server = new FakeCollabServer();
    const a = open(server, ann);
    await vi.waitFor(() => expect(a.state.ready).toBe(true));
    const b = open(server, bob);
    await vi.waitFor(() => expect(b.state.ready).toBe(true));
    titleOf(a.state.doc!).insert(5, " for 2027");
    await vi.waitFor(() => expect(titleIn(b)).toBe("Plans for 2027"));
    titleOf(b.state.doc!).insert(0, "Our ");
    await vi.waitFor(() => expect(titleIn(a)).toBe("Our Plans for 2027"));
  });

  it("merges what was written offline with what the others wrote meanwhile", async () => {
    const server = new FakeCollabServer();
    let bobOnline = true;
    const a = open(server, ann);
    await vi.waitFor(() => expect(a.state.ready).toBe(true));
    const b = open(server, bob, seedWith("x"), () => bobOnline);
    await vi.waitFor(() => expect(b.state.ready).toBe(true));

    bobOnline = false;
    window.dispatchEvent(new Event("offline"));
    await vi.waitFor(() => expect(b.state.status).toBe("offline"));
    titleOf(b.state.doc!).insert(5, " offline");
    titleOf(a.state.doc!).insert(0, "Shared ");
    await vi.waitFor(() => expect(titleOf(server.doc()).toString()).toBe("Shared Plans"));
    expect(titleIn(b)).toBe("Plans offline");

    bobOnline = true;
    window.dispatchEvent(new Event("online"));
    await vi.waitFor(() => expect(b.state.status).toBe("live"));
    await vi.waitFor(() => expect(titleIn(b)).toBe("Shared Plans offline"));
    await vi.waitFor(() => expect(titleIn(a)).toBe("Shared Plans offline"));
    expect(titleOf(server.doc()).toString()).toBe("Shared Plans offline");
  });

  it("follows a publish from anybody's browser", async () => {
    const server = new FakeCollabServer();
    const a = open(server, ann);
    await vi.waitFor(() => expect(a.state.ready).toBe(true));
    const b = open(server, bob);
    await vi.waitFor(() => expect(b.state.ready).toBe(true));
    a.published(2);
    await vi.waitFor(() => expect(b.state.base).toBe(2));
    expect(a.state.base).toBe(2);
    expect(server.published).toEqual([2]);
  });

  it("starts afresh when the room is thrown away, seeding from the page", async () => {
    const server = new FakeCollabServer();
    const annSeed = seedWith("Plans");
    const a = open(server, ann, annSeed);
    await vi.waitFor(() => expect(a.state.ready).toBe(true));
    const first = a.state.doc;
    a.discard();
    await vi.waitFor(() => expect(a.state.room).toBe("room-2"));
    await vi.waitFor(() => expect(a.state.ready).toBe(true));
    expect(a.state.doc).not.toBe(first);
    expect(annSeed).toHaveBeenLastCalledWith(true);
  });

  it("stops for good once its person may no longer edit", async () => {
    const server = new FakeCollabServer();
    const a = open(server, ann);
    await vi.waitFor(() => expect(a.state.ready).toBe(true));
    server.drop(CLOSE.refused);
    await vi.waitFor(() => expect(a.state.stopped).toBe("refused"));
    expect(a.state.status).toBe("stopped");
    expect(a.state.ready).toBe(false);
  });

  it("merges a long room's updates when asked", async () => {
    const server = new FakeCollabServer();
    const a = open(server, ann);
    await vi.waitFor(() => expect(a.state.ready).toBe(true));
    for (const word of [" one", " two", " three"]) titleOf(a.state.doc!).insert(titleOf(a.state.doc!).length, word);
    await vi.waitFor(() => expect(server.updates.length).toBe(4));
    server.compactAt = 4;
    const b = open(server, bob);
    await vi.waitFor(() => expect(server.compactions.length).toBe(1));
    await vi.waitFor(() => expect(b.state.ready).toBe(true));
    expect(server.updates.length).toBe(1);
    const merged = new Y.Doc();
    Y.applyUpdate(merged, server.compactions[0]!);
    expect(titleOf(merged).toString()).toBe("Plans one two three");
  });

  it("shows who else is in the room, and forgets them when they go", async () => {
    const server = new FakeCollabServer();
    const a = open(server, ann);
    await vi.waitFor(() => expect(a.state.ready).toBe(true));
    const b = open(server, bob);
    await vi.waitFor(() => expect(b.state.ready).toBe(true));
    const names = () => [...(a.state.awareness?.getStates().values() ?? [])].map((s) => (s as { user?: CollabUser }).user?.name).sort();
    await vi.waitFor(() => expect(names()).toEqual(["Ann", "Bob"]));
    server.drop(1006);
    await vi.waitFor(() => expect(names()).toEqual(["Ann"]));
  });
});
