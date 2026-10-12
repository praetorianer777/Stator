// What every scenario shares: where Stator is, how to sign in, the pages the
// load runs against, and the framing of the shared draft's WebSocket.
import http from "k6/http";
import { check, fail } from "k6";

export const BASE = __ENV.STATOR_URL || "http://web";
export const API = `${BASE}/api/v1`;
export const SPACE = __ENV.LOAD_SPACE || "LOAD";
export const PAGES = Number(__ENV.LOAD_PAGES || 2000);
// Children per page in the seeded tree: 2000 pages are five levels deep.
const BRANCHING = 6;
// Every editor saves and publishes these few, so their versions collide.
export const HOT_PAGES = 5;
// Pages held open as shared drafts by the live scenario. Each api process
// takes at most 50 connections to one page, so the load spreads over these.
export const LIVE_PAGES = 4;
const SEED_BATCH = 20;
const COMMENT_EVERY = 10;
const ATTACHMENT_EVERY = 50;

// Filler that every page shares, and topic words, a few to a page: a query
// for a topic finds a few dozen pages, one for filler nearly all of them.
const FILLER = ("the team checks the plan before each release and writes down what changed " +
  "so that everybody knows which steps are done and which are still open").split(" ");
const SYLLABLES = "ka lo mi ne ru sa ti vo be da fe gu hi jo ky pe qua ro su ze".split(" ");
export const TOPICS = SYLLABLES.length * SYLLABLES.length;
const TOPICS_PER_PARAGRAPH = 3;

export function word(i) {
  const n = i % TOPICS;
  return `${SYLLABLES[Math.floor(n / SYLLABLES.length)]}${SYLLABLES[n % SYLLABLES.length]}n`;
}

export function filler(i) {
  return FILLER[i % FILLER.length];
}

export function doc(seed, paragraphs) {
  const content = [];
  for (let p = 0; p < paragraphs; p++) {
    const words = [];
    for (let w = 0; w < 40; w++) words.push(filler(w + p));
    for (let t = 0; t < TOPICS_PER_PARAGRAPH; t++) words.splice((t + 1) * 9, 0, word(seed * 11 + p * 5 + t));
    content.push({ type: "paragraph", content: [{ type: "text", text: `${words.join(" ")}.` }] });
  }
  return { type: "doc", content };
}

// kind groups requests for the thresholds: read, tree, search, write or seed.
export function params(cookie, name, kind = "seed", extra = {}) {
  return {
    headers: { Cookie: cookie, "Content-Type": "application/json" },
    tags: { name, kind },
    ...extra,
  };
}

function ok(res, what) {
  if (!check(res, { [what]: (r) => r.status >= 200 && r.status < 300 })) {
    fail(`${what}: ${res.status} ${res.body && res.body.slice(0, 300)}`);
  }
  return res;
}

// One session for every virtual user: the bootstrap administrator's, the one
// account that signs in with a password.
export function signIn() {
  const res = ok(
    http.post(
      `${API}/auth/login`,
      JSON.stringify({
        email: __ENV.LOAD_EMAIL || "admin@stator.test",
        password: __ENV.LOAD_PASSWORD || "stator admin password",
      }),
      { headers: { "Content-Type": "application/json" } },
    ),
    "sign in",
  );
  const name = Object.keys(res.cookies).find((c) => c.includes("session"));
  if (!name) fail("signing in set no session cookie");
  return `${name}=${res.cookies[name][0].value}`;
}

function outline(cookie) {
  return ok(http.get(`${API}/spaces/${SPACE}/outline`, params(cookie, "seed outline")), "read the outline").json("pages");
}

// The space and its pages, made once and reused by every later run.
export function seed(cookie) {
  let res = http.get(`${API}/spaces/${SPACE}`, params(cookie, "seed space"));
  if (res.status === 404) {
    ok(
      http.post(`${API}/spaces`, JSON.stringify({ key: SPACE, name: "Load test" }), params(cookie, "create space")),
      "create the load space",
    );
    res = http.get(`${API}/spaces/${SPACE}`, params(cookie, "seed space"));
  }
  const home = ok(res, "read the load space").json("space.homePageId");

  let pages = outline(cookie).filter((p) => p.id !== home);
  if (pages.length < PAGES) {
    console.log(`seeding ${PAGES - pages.length} pages into ${SPACE}`);
    // A page's parent is made before it: the nth page hangs below the
    // (n / BRANCHING)th, and a batch stops where its parents run out.
    const parents = [home, ...pages.map((p) => p.id)];
    let made = pages.length;
    while (made < PAGES) {
      const batch = [];
      for (let n = made; n < PAGES && batch.length < SEED_BATCH && Math.floor(n / BRANCHING) < parents.length; n++) {
        batch.push([
          "POST",
          `${API}/pages`,
          JSON.stringify({
            parentId: parents[Math.floor(n / BRANCHING)],
            title: `${word(n * 11)} ${filler(n)} ${n}`,
            body: doc(n, 3 + (n % 8)),
            publish: true,
          }),
          params(cookie, "seed page"),
        ]);
      }
      for (const r of http.batch(batch)) parents.push(ok(r, "seed a page").json("page.id"));
      made += batch.length;
    }
    pages = outline(cookie).filter((p) => p.id !== home);
    seedExtras(cookie, pages);
  }
  return { home, pages: pages.map((p) => p.id) };
}

function seedExtras(cookie, pages) {
  const comments = [];
  for (let i = 0; i < pages.length; i += COMMENT_EVERY) {
    comments.push([
      "POST",
      `${API}/pages/${pages[i].id}/comments`,
      JSON.stringify({ body: doc(i, 1) }),
      params(cookie, "seed comment"),
    ]);
  }
  for (let i = 0; i < comments.length; i += SEED_BATCH) {
    for (const r of http.batch(comments.slice(i, i + SEED_BATCH))) ok(r, "seed a comment");
  }
  const file = "x".repeat(32 * 1024);
  for (let i = 0; i < pages.length; i += ATTACHMENT_EVERY) {
    ok(
      http.post(
        `${API}/pages/${pages[i].id}/attachments`,
        { file: http.file(file, `${word(i)}-notes-${i}.txt`, "text/plain") },
        { headers: { Cookie: cookie }, tags: { name: "seed attachment", kind: "seed" } },
      ),
      "seed an attachment",
    );
  }
}

// lib0's encoding, as web/src/features/collab and backend/internal/collab use it.
function varuint(out, n) {
  while (n > 0x7f) {
    out.push((n & 0x7f) | 0x80);
    n = Math.floor(n / 128);
  }
  out.push(n);
}

function varbytes(out, bytes) {
  varuint(out, bytes.length);
  for (const b of bytes) out.push(b);
}

// Only ASCII is ever sent, so a character is its byte.
function ascii(s) {
  const out = [];
  for (let i = 0; i < s.length; i++) out.push(s.charCodeAt(i) & 0x7f);
  return out;
}

const MSG_SYNC = 0;
const MSG_AWARENESS = 1;
const MSG_DISCARD = 103;
const SYNC_UPDATE = 2;
// Marks the load's own updates, so a client can tell them from the room's
// history and time their delivery. The server stores updates unread, so these
// need not be Yjs; the room is thrown away when the run ends.
const MARK = [0x6c, 0x6f, 0x61, 0x64];

export function updateFrame(sentAt) {
  const body = [...MARK, ...ascii(String(sentAt)), ...new Array(200).fill(0x2e)];
  const out = [];
  varuint(out, MSG_SYNC);
  varuint(out, SYNC_UPDATE);
  varbytes(out, body);
  return new Uint8Array(out).buffer;
}

export function awarenessFrame(client, clock, state) {
  const peers = [];
  varuint(peers, 1);
  varuint(peers, client);
  varuint(peers, clock);
  varbytes(peers, ascii(JSON.stringify(state)));
  const out = [];
  varuint(out, MSG_AWARENESS);
  varbytes(out, peers);
  return new Uint8Array(out).buffer;
}

export function discardFrame() {
  return new Uint8Array([MSG_DISCARD]).buffer;
}

const MSG_ROOM = 100;
const MSG_COMPACT = 104;
const MSG_COMPACTED = 105;
const SYNC_STEP2 = 1;

// What the live scenario needs of a frame from the server: whether it starts
// or ends a load of the room, asks for a compaction, or is one of the load's
// updates and when that was sent.
export function readFrame(data) {
  const b = new Uint8Array(data);
  let i = 0;
  const read = () => {
    let n = 0;
    let shift = 1;
    for (;;) {
      const byte = b[i++];
      n += (byte & 0x7f) * shift;
      if (byte < 0x80 || i >= b.length) return n;
      shift *= 128;
    }
  };
  const type = read();
  if (type === MSG_ROOM) return { roomStart: true };
  if (type === MSG_COMPACT) return { compact: { from: read(), to: read(), count: read() } };
  if (type !== MSG_SYNC) return {};
  const sub = read();
  if (sub === SYNC_STEP2) return { loaded: true };
  if (sub !== SYNC_UPDATE) return {};
  const len = read();
  for (let m = 0; m < MARK.length; m++) if (b[i + m] !== MARK[m]) return {};
  let digits = "";
  for (let j = i + MARK.length; j < i + len && b[j] >= 0x30 && b[j] <= 0x39; j++) digits += String.fromCharCode(b[j]);
  return digits ? { sentAt: Number(digits) } : {};
}

// The merge a browser answers a compaction with; like the updates, unread.
export function compactedFrame({ from, to, count }) {
  const out = [];
  varuint(out, MSG_COMPACTED);
  varuint(out, from);
  varuint(out, to);
  varuint(out, count);
  varbytes(out, new Array(256).fill(0x2e));
  return new Uint8Array(out).buffer;
}
