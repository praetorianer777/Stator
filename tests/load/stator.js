// Load on one Stator deployment: readers, editors and people editing a page
// together, at the level LOAD_PROFILE names. docs/load-testing.md says what
// each models and what the thresholds mean. Run through make test-load.
import exec from "k6/execution";
import { WebSocket } from "k6/experimental/websockets";
import http from "k6/http";
import { Counter, Gauge, Trend } from "k6/metrics";
import { check, sleep } from "k6";
import { setInterval, clearInterval, setTimeout } from "k6/timers";
import {
  API,
  BASE,
  HOT_PAGES,
  LIVE_PAGES,
  SPACE,
  TOPICS,
  awarenessFrame,
  discardFrame,
  doc,
  filler,
  params,
  seed,
  compactedFrame,
  readFrame,
  signIn,
  updateFrame,
  word,
} from "./lib.js";

const PROFILE = __ENV.LOAD_PROFILE || "standard";
// Kinds of request to leave out, such as search,tree while those are slow
// enough to take the machine from everything else (#370, #371).
const SKIP = (__ENV.LOAD_SKIP || "").split(",").filter(Boolean);
const SCENARIOS = (__ENV.LOAD_SCENARIOS || "readers,editors,live").split(",");

// How long one person keeps a shared draft open, give or take a quarter, and
// how often they move their cursor and type, as a browser sends them. People
// arrive over the first seconds rather than in one instant.
const LIVE_SESSION_MS = 60000;
const LIVE_ARRIVAL_S = 10;
const AWARENESS_EVERY_MS = 1000;
const UPDATE_EVERY_MS = 2000;
// One in this many reader visits reloads the whole tree, as a cold start does.
const OUTLINE_EVERY = 5;
const SEARCH_EVERY = 3;
// The share of searches for a word nearly every page holds.
const BROAD_SEARCHES = 0.1;
const COMMENT_EVERY = 4;
// The share of editors' saves that go to the few pages everybody edits.
const HOT_SHARE = 0.3;

// How many shared drafts the live scenario spreads its people over, so that a
// room always has somebody to hear an update.
const LIVE_ROOMS = { smoke: 1, standard: LIVE_PAGES, ceiling: LIVE_PAGES }[PROFILE];

const delivery = new Trend("collab_delivery", true);
const conflicts = new Counter("edit_conflicts");
const active = new Gauge("load_vus");

const PROFILES = {
  // A minute with a few people, to prove the scripts and the stack agree.
  smoke: {
    readers: { executor: "constant-vus", vus: 3, duration: "1m" },
    editors: { executor: "constant-vus", vus: 1, duration: "1m" },
    live: { executor: "constant-vus", vus: 4, duration: "1m" },
  },
  // A busy team: the level the thresholds hold for.
  standard: {
    readers: { executor: "constant-vus", vus: 40, duration: "3m" },
    editors: { executor: "constant-vus", vus: 8, duration: "3m" },
    live: { executor: "constant-vus", vus: 24, duration: "3m" },
  },
  // Up until a threshold breaks, which stops the run: load_vus says how many
  // people that took.
  ceiling: {
    readers: { executor: "ramping-vus", startVUs: 10, stages: [{ duration: "15m", target: 600 }] },
    editors: { executor: "ramping-vus", startVUs: 2, stages: [{ duration: "15m", target: 90 }] },
    live: { executor: "ramping-vus", startVUs: 4, stages: [{ duration: "15m", target: 160 }] },
  },
};

if (!PROFILES[PROFILE]) throw new Error(`LOAD_PROFILE is ${PROFILE}; use smoke, standard or ceiling.`);

// In the ceiling profile a breaking threshold stops the run, except search
// and the whole tree: they are slow at any load (#370, #371) and would end
// the ramp before it measured anything else.
const limit = (threshold, stops = true) =>
  PROFILE === "ceiling" && stops ? [{ threshold, abortOnFail: true, delayAbortEval: "30s" }] : [threshold];

const THRESHOLDS = {
  readers: {
    "http_req_duration{kind:read}": limit("p(95)<300"),
    "http_req_duration{kind:tree}": limit("p(95)<1000", false),
    "http_req_duration{kind:search}": limit("p(95)<800", false),
    "http_req_failed{scenario:readers}": limit("rate<0.01"),
  },
  editors: {
    "http_req_duration{kind:write}": limit("p(95)<1000"),
    "http_req_failed{scenario:editors}": limit("rate<0.01"),
  },
  live: {
    "collab_delivery{scenario:live}": limit("p(95)<300"),
    "ws_connecting{scenario:live}": limit("p(95)<500"),
    "checks{scenario:live}": limit("rate>0.99"),
  },
};

// Each request on its own in the summary; the condition never fails.
const BREAKDOWN = Object.fromEntries(
  ["space", "outline", "children", "page", "search", "save draft", "publish", "comment"].map((name) => [
    `http_req_duration{name:${name}}`,
    ["max>=0"],
  ]),
);

export const options = {
  // A live session runs on past the end of the profile by up to its length.
  scenarios: Object.fromEntries(
    SCENARIOS.map((name) => [name, { ...PROFILES[PROFILE][name], exec: name, gracefulStop: "80s" }]),
  ),
  thresholds: Object.fromEntries(
    Object.entries(Object.assign({}, BREAKDOWN, ...SCENARIOS.map((name) => THRESHOLDS[name]))).filter(
      ([metric]) => !SKIP.some((kind) => metric.includes(`kind:${kind}`) || metric.includes(`name:${kind === "tree" ? "outline" : kind}`)),
    ),
  ),
  setupTimeout: "15m",
  teardownTimeout: "2m",
  summaryTrendStats: ["avg", "med", "p(95)", "p(99)", "max"],
};

function pick(list) {
  return list[Math.floor(Math.random() * list.length)];
}

export function setup() {
  const cookie = signIn();
  const { pages } = seed(cookie);
  return {
    cookie,
    pages,
    hot: pages.slice(0, HOT_PAGES),
    live: pages.slice(HOT_PAGES, HOT_PAGES + LIVE_PAGES),
    quiet: pages.slice(HOT_PAGES + LIVE_PAGES),
  };
}

export function readers(data) {
  active.add(exec.instance.vusActive);
  const p = (name, kind = "read") => params(data.cookie, name, kind);
  http.get(`${API}/spaces/${SPACE}`, p("space"));
  if (!SKIP.includes("tree") && exec.scenario.iterationInTest % OUTLINE_EVERY === 0) {
    http.get(`${API}/spaces/${SPACE}/outline`, p("outline", "tree"));
  }
  const page = pick(data.pages);
  http.get(`${API}/spaces/${SPACE}/pages?parent=${page}`, p("children"));
  http.get(`${API}/pages/${page}`, p("page"));
  if (!SKIP.includes("search") && exec.scenario.iterationInTest % SEARCH_EVERY === 0) {
    const q = Math.random() < BROAD_SEARCHES ? filler(Math.floor(Math.random() * 100)) : word(Math.floor(Math.random() * TOPICS));
    http.get(`${API}/search?q=${encodeURIComponent(q)}&space=${SPACE}`, p("search", "search"));
  }
  sleep(1 + Math.random() * 2);
}

// A stale base version is answered 409: the conflict the hot pages are there
// to cause, not a failure.
const conflictsExpected = http.expectedStatuses({ min: 200, max: 299 }, 409);

export function editors(data) {
  active.add(exec.instance.vusActive);
  const id = Math.random() < HOT_SHARE ? pick(data.hot) : pick(data.quiet);
  const res = http.get(`${API}/pages/${id}`, params(data.cookie, "page", "read"));
  if (!check(res, { "page read": (r) => r.status === 200 })) return;
  const page = res.json("page");

  const save = http.put(
    `${API}/pages/${id}/draft`,
    JSON.stringify({ title: page.title, body: doc(Math.floor(Math.random() * 1e6), 4), baseVersion: page.version }),
    params(data.cookie, "save draft", "write", { responseCallback: conflictsExpected }),
  );
  if (save.status === 409) {
    conflicts.add(1);
  } else {
    const publish = http.post(
      `${API}/pages/${id}/publish`,
      JSON.stringify({ comment: "load test" }),
      params(data.cookie, "publish", "write", { responseCallback: conflictsExpected }),
    );
    if (publish.status === 409) conflicts.add(1);
  }
  if (exec.scenario.iterationInTest % COMMENT_EVERY === 0) {
    http.post(`${API}/pages/${id}/comments`, JSON.stringify({ body: doc(1, 1) }), params(data.cookie, "comment", "write"));
  }
  sleep(2 + Math.random() * 3);
}

function collabURL(page) {
  return `${BASE.replace(/^http/, "ws")}/api/v1/pages/${page}/collab`;
}

export function live(data) {
  if (exec.vu.iterationInScenario === 0) sleep(Math.random() * LIVE_ARRIVAL_S);
  active.add(exec.instance.vusActive);
  const page = data.live[exec.vu.idInTest % LIVE_ROOMS];
  const client = exec.vu.idInTest * 1000 + (exec.vu.iterationInScenario % 1000);
  const ws = new WebSocket(collabURL(page), null, { headers: { Cookie: data.cookie }, tags: { name: "collab" } });
  ws.binaryType = "arraybuffer";
  const timers = [];
  let clock = 0;
  const stop = () => {
    for (const t of timers) clearInterval(t);
  };
  ws.onopen = () => {
    check(ws, { "shared draft opened": () => true });
    timers.push(
      setInterval(() => {
        clock++;
        ws.send(awarenessFrame(client, clock, { user: { name: `Load ${client}` }, cursor: clock }));
      }, AWARENESS_EVERY_MS),
    );
    timers.push(setInterval(() => ws.send(updateFrame(Date.now())), UPDATE_EVERY_MS));
    setTimeout(
      () => {
        stop();
        ws.close();
      },
      LIVE_SESSION_MS * (0.75 + Math.random() * 0.5),
    );
  };
  // The room's history arrives first, after every room frame; only what is
  // relayed once a load has ended says how fast an update travels.
  let loaded = false;
  ws.onmessage = (e) => {
    const f = readFrame(e.data);
    if (f.roomStart) loaded = false;
    if (f.loaded) loaded = true;
    if (f.compact) ws.send(compactedFrame(f.compact));
    if (loaded && f.sentAt) delivery.add(Date.now() - f.sentAt);
  };
  ws.onerror = (e) => {
    check(ws, { "shared draft opened": () => false });
    console.warn(`collab ${page}: ${e.error}`);
    stop();
  };
}

// The load's updates are not Yjs, so nobody may load those rooms afterwards.
export function teardown(data) {
  for (const page of data.live) {
    const ws = new WebSocket(collabURL(page), null, { headers: { Cookie: data.cookie } });
    ws.onopen = () => {
      ws.send(discardFrame());
      setTimeout(() => ws.close(), 500);
    };
  }
}
