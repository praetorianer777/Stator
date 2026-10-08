import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { Awareness } from "y-protocols/awareness";
import {
  PRESENCE_FIELD,
  SketchLink,
  keepShared,
  sharedIsAhead,
  sharedScene,
  sketchCollaborators,
  sketchDrawers,
  sketchMap,
  supersedes,
  type SketchCanvas,
} from "./live";
import { readScene, type SketchElement } from "./scene";

const SKETCH = "5f0c6a2e-9a8b-4c1d-8e2f-0a1b2c3d4e5f";

function shape(id: string, version: number, versionNonce: number, extra: Record<string, unknown> = {}): SketchElement {
  return { id, type: "rectangle", x: 0, y: 0, width: 10, height: 10, index: `a${id}`, version, versionNonce, isDeleted: false, ...extra };
}

/** A canvas as Excalidraw keeps one: each shape once, the copy Excalidraw would keep. */
class Canvas implements SketchCanvas {
  shapes = new Map<string, SketchElement>();
  bg = "#ffffff";
  applied = 0;
  elements = () => [...this.shapes.values()];
  background = () => this.bg;
  apply = (remote: SketchElement[], background: string | null) => {
    this.applied++;
    for (const element of remote) if (supersedes(element, this.shapes.get(element.id))) this.shapes.set(element.id, element);
    if (background) this.bg = background;
  };
  draw(element: SketchElement) {
    this.shapes.set(element.id, element);
  }
  visible() {
    return this.elements()
      .filter((e) => !e.isDeleted)
      .map((e) => e.id)
      .sort();
  }
}

/** Two browsers' shared drafts, passing every update on while both are online. */
function pair() {
  const a = new Y.Doc();
  const b = new Y.Doc();
  let online = true;
  const pending: Array<[Y.Doc, Uint8Array]> = [];
  const relay = (to: Y.Doc) => (update: Uint8Array, origin: unknown) => {
    if (origin === "remote") return;
    if (online) Y.applyUpdate(to, update, "remote");
    else pending.push([to, update]);
  };
  a.on("update", relay(b));
  b.on("update", relay(a));
  return {
    a,
    b,
    offline() {
      online = false;
    },
    online() {
      online = true;
      for (const [to, update] of pending.splice(0)) Y.applyUpdate(to, update, "remote");
    },
  };
}

describe("supersedes", () => {
  it("prefers the later version, and of equal versions the lower nonce, as Excalidraw does", () => {
    expect(supersedes(shape("x", 3, 9), shape("x", 2, 1))).toBe(true);
    expect(supersedes(shape("x", 2, 1), shape("x", 3, 9))).toBe(false);
    expect(supersedes(shape("x", 2, 1), shape("x", 2, 9))).toBe(true);
    expect(supersedes(shape("x", 2, 9), shape("x", 2, 1))).toBe(false);
    expect(supersedes(shape("x", 2, 1), shape("x", 2, 1))).toBe(false);
    expect(supersedes(shape("x", 1, 0), undefined)).toBe(true);
    // A scene written by hand has no versions; Excalidraw reads them as 1 and 0.
    expect(supersedes({ id: "x", type: "rectangle" }, shape("x", 1, 0))).toBe(false);
  });
});

describe("a sketch drawn together", () => {
  it("shows each person's shapes on the other's canvas, and the first person in brings the saved ones", () => {
    const { a, b } = pair();
    const ada = new Canvas();
    ada.draw(shape("saved", 4, 1));
    const adaLink = new SketchLink(a, SKETCH, ada, 0);
    const ben = new Canvas();
    const benLink = new SketchLink(b, SKETCH, ben, 0);
    expect(ben.visible()).toEqual(["saved"]);

    ada.draw(shape("box", 1, 5));
    ben.draw(shape("ring", 1, 7, { type: "ellipse" }));
    adaLink.push();
    benLink.push();
    expect(ada.visible()).toEqual(["box", "ring", "saved"]);
    expect(ben.visible()).toEqual(["box", "ring", "saved"]);
  });

  it("settles two changes to one shape on the copy Excalidraw keeps, on both canvases", () => {
    const pairs = [
      [shape("box", 2, 900, { x: 1 }), shape("box", 2, 100, { x: 2 }), 2],
      [shape("box", 3, 100, { x: 3 }), shape("box", 3, 900, { x: 4 }), 3],
      [shape("box", 5, 500, { x: 5 }), shape("box", 4, 1, { x: 6 }), 5],
    ] as const;
    for (const [mine, theirs, x] of pairs) {
      const p = pair();
      // Both change it while apart, so neither sees the other's first.
      const one = new Canvas();
      const two = new Canvas();
      one.draw(shape("box", 1, 0));
      const oneLink = new SketchLink(p.a, SKETCH, one, 0);
      const twoLink = new SketchLink(p.b, SKETCH, two, 0);
      p.offline();
      one.draw(mine);
      two.draw(theirs);
      oneLink.push();
      twoLink.push();
      p.online();
      expect(one.shapes.get("box")?.x, `one, ${x}`).toBe(x);
      expect(two.shapes.get("box")?.x, `two, ${x}`).toBe(x);
      expect(sketchMap(p.a, SKETCH).get("e:box")).toEqual(sketchMap(p.b, SKETCH).get("e:box"));
      expect((sketchMap(p.a, SKETCH).get("e:box") as SketchElement).x).toBe(x);
    }
  });

  it("deletes a shape everywhere, and keeps it out of the scene the page saves", () => {
    const { a, b } = pair();
    const ada = new Canvas();
    const ben = new Canvas();
    ada.draw(shape("box", 1, 0));
    ada.draw(shape("ring", 1, 0));
    const adaLink = new SketchLink(a, SKETCH, ada, 0);
    new SketchLink(b, SKETCH, ben, 0);
    ada.draw(shape("box", 2, 3, { isDeleted: true }));
    adaLink.push();
    expect(ben.visible()).toEqual(["ring"]);
    expect(readScene(keepShared(sketchMap(b, SKETCH))!.scene)?.elements.map((e) => e.id)).toEqual(["ring"]);
  });

  it("merges what was drawn offline when the connection returns", () => {
    const { a, b, offline, online } = pair();
    const ada = new Canvas();
    const ben = new Canvas();
    const adaLink = new SketchLink(a, SKETCH, ada, 0);
    const benLink = new SketchLink(b, SKETCH, ben, 0);
    offline();
    ben.draw(shape("written-offline", 1, 2));
    benLink.push();
    ada.draw(shape("written-online", 1, 4));
    adaLink.push();
    expect(ada.visible()).toEqual(["written-online"]);
    online();
    expect(ada.visible()).toEqual(["written-offline", "written-online"]);
    expect(ben.visible()).toEqual(["written-offline", "written-online"]);
  });

  it("gives somebody who opens it later the scene as it stands, background included", () => {
    const { a } = pair();
    const ada = new Canvas();
    const adaLink = new SketchLink(a, SKETCH, ada, 0);
    ada.draw(shape("box", 1, 0));
    ada.draw(shape("gone", 2, 0, { isDeleted: true }));
    ada.bg = "#fff3bf";
    adaLink.push();

    const late = new Y.Doc();
    Y.applyUpdate(late, Y.encodeStateAsUpdate(a));
    const cleo = new Canvas();
    new SketchLink(late, SKETCH, cleo, 0);
    expect(cleo.visible()).toEqual(["box"]);
    expect(cleo.bg).toBe("#fff3bf");
    expect(sharedScene(sketchMap(late, SKETCH))?.appState.viewBackgroundColor).toBe("#fff3bf");
  });

  it("shares no pictures or other programs' data, and keeps its copy apart from the canvas's", () => {
    const doc = new Y.Doc();
    const canvas = new Canvas();
    const box = shape("box", 1, 0, { customData: { other: 1 }, link: "javascript:alert(1)" });
    canvas.draw(box);
    canvas.draw({ ...shape("picture", 1, 0), type: "image", fileId: "f1" });
    const link = new SketchLink(doc, SKETCH, canvas, 0);
    const map = sketchMap(doc, SKETCH);
    expect(map.has("e:picture")).toBe(false);
    const kept = map.get("e:box") as SketchElement;
    expect(kept.customData).toBeUndefined();
    expect(kept.link).toBeNull();
    // Excalidraw changes its elements in place; the shared copy must not follow unseen.
    box.version = 2;
    box.x = 50;
    expect((map.get("e:box") as SketchElement).x).toBe(0);
    link.push();
    expect((map.get("e:box") as SketchElement).x).toBe(50);
  });

  it("goes out once the interval has passed, however often the canvas changes", async () => {
    const doc = new Y.Doc();
    const canvas = new Canvas();
    const link = new SketchLink(doc, SKETCH, canvas, 10);
    let updates = 0;
    doc.on("update", () => updates++);
    for (let v = 1; v <= 20; v++) {
      canvas.draw(shape("line", v, 0));
      link.changed();
    }
    expect(updates).toBe(0);
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(updates).toBe(1);
    expect((sketchMap(doc, SKETCH).get("e:line") as SketchElement).version).toBe(20);
  });

  it("stops taking changes in once the canvas closes", () => {
    const { a, b } = pair();
    const ada = new Canvas();
    const ben = new Canvas();
    const adaLink = new SketchLink(a, SKETCH, ada, 0);
    const benLink = new SketchLink(b, SKETCH, ben, 0);
    benLink.destroy();
    ada.draw(shape("box", 1, 0));
    adaLink.push();
    expect(ben.visible()).toEqual([]);
  });
});

describe("sharedIsAhead", () => {
  const saved = (elements: SketchElement[], background = "#ffffff") => JSON.stringify({ elements, appState: { viewBackgroundColor: background } });

  it("says the page's drawing is behind only when a shape or the background changed", () => {
    const doc = new Y.Doc();
    const map = sketchMap(doc, SKETCH);
    expect(sharedIsAhead(map, saved([]))).toBe(false);
    const canvas = new Canvas();
    canvas.draw(shape("box", 1, 0));
    const link = new SketchLink(doc, SKETCH, canvas, 0);
    expect(sharedIsAhead(map, saved([shape("box", 1, 0)]))).toBe(false);
    // A scene written by hand, without versions, is version 1 to Excalidraw.
    expect(sharedIsAhead(map, saved([{ id: "box", type: "rectangle" }]))).toBe(false);
    expect(sharedIsAhead(map, saved([shape("box", 1, 0)], "#000000"))).toBe(true);
    expect(sharedIsAhead(map, "not a scene")).toBe(true);
    canvas.draw(shape("box", 2, 7));
    link.push();
    expect(sharedIsAhead(map, saved([shape("box", 1, 0)]))).toBe(true);
    canvas.draw(shape("box", 3, 7, { isDeleted: true }));
    link.push();
    expect(sharedIsAhead(map, saved([]))).toBe(false);
  });
});

describe("who is on a sketch", () => {
  it("names everybody else with it open, in Excalidraw's shape, and nobody on another", () => {
    const states = new Map<number, Record<string, unknown>>([
      [1, { user: { id: "u-ada", name: "Ada", color: "#1d4ed8" }, [PRESENCE_FIELD]: { id: SKETCH, pointer: null, button: "up", selected: [] } }],
      [
        2,
        {
          user: { id: "u-ben", name: "Ben", color: "#b45309" },
          [PRESENCE_FIELD]: { id: SKETCH, pointer: { x: 4, y: 5, tool: "pointer" }, button: "down", selected: ["box"] },
        },
      ],
      [3, { user: { id: "u-cleo", name: "Cleo", color: "#047857" }, [PRESENCE_FIELD]: { id: "another", pointer: null, button: "up", selected: [] } }],
      [4, { user: { id: "u-dan", name: "Dan", color: "#be185d" }, [PRESENCE_FIELD]: null }],
      [5, { user: { id: "u-ben", name: "Ben", color: "#b45309" }, [PRESENCE_FIELD]: { id: SKETCH, pointer: null, button: "up", selected: [] } }],
    ]);
    const seen = sketchCollaborators(states, 1, SKETCH);
    expect([...seen.keys()]).toEqual(["2", "5"]);
    expect(seen.get("2")).toEqual({
      id: "u-ben",
      username: "Ben",
      color: { background: "#b45309", stroke: "#b45309" },
      pointer: { x: 4, y: 5, tool: "pointer" },
      button: "down",
      selectedElementIds: { box: true },
    });
    expect(sketchDrawers(states, 1, SKETCH)).toEqual([{ id: "u-ben", name: "Ben", color: "#b45309" }]);
    expect(sketchDrawers(states, 3, "another")).toEqual([]);
  });

  it("reads what a browser's awareness says", () => {
    const doc = new Y.Doc();
    const awareness = new Awareness(doc);
    awareness.setLocalStateField("user", { id: "u-ada", name: "Ada", color: "#1d4ed8" });
    awareness.setLocalStateField(PRESENCE_FIELD, { id: SKETCH, pointer: null, button: "up", selected: [] });
    const states = awareness.getStates() as Map<number, Record<string, unknown>>;
    expect(sketchCollaborators(states, awareness.clientID, SKETCH).size).toBe(0);
    expect(sketchCollaborators(states, awareness.clientID + 1, SKETCH).get(String(awareness.clientID))?.username).toBe("Ada");
    awareness.destroy();
  });
});
