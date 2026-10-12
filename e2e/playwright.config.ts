import { defineConfig, devices, type Project, type ReporterDescription } from "@playwright/test";
import { INSTANCE_SPECS, RESETS_INSTANCE } from "./fixtures/projects";
import { WEB_URL } from "./fixtures/stack";

// A spec that only means something at one width carries @desktop or @mobile
// and runs in that project alone; everything else runs in both.
const MOBILE_VIEWPORT = { width: 360, height: 740 };

const INSTANCE_FILES = new RegExp(`/(${INSTANCE_SPECS.join("|")})\\.spec\\.ts$`);

// `make test-e2e` runs the suite in two phases, the parallel specs and then
// the instance ones with one worker, since a project depending on another
// would run the other whole in every shard. Without E2E_PHASE every project
// is there, for running one spec by hand.
type Phase = "parallel" | "instance";
const PHASE = process.env.E2E_PHASE as Phase | undefined;
if (PHASE !== undefined && PHASE !== "parallel" && PHASE !== "instance") {
  throw new Error(`E2E_PHASE is ${PHASE}; set it to parallel or instance, or leave it out.`);
}

const desktop = { ...devices["Desktop Chrome"] };
const mobile = { ...devices["Desktop Chrome"], viewport: MOBILE_VIEWPORT, isMobile: true, hasTouch: true };

const parallelProjects: Project[] = [
  { name: "chromium", dependencies: ["setup"], grepInvert: /@mobile/, testIgnore: INSTANCE_FILES, use: desktop },
  { name: "mobile", dependencies: ["setup"], grepInvert: /@desktop/, testIgnore: INSTANCE_FILES, use: mobile },
];
const instance = { dependencies: ["setup"], testMatch: INSTANCE_FILES, fullyParallel: false, metadata: { [RESETS_INSTANCE]: true } };
const instanceProjects: Project[] = [
  { ...instance, name: "instance", grepInvert: /@mobile/, use: desktop },
  { ...instance, name: "instance-mobile", grepInvert: /@desktop/, use: mobile },
];

// CI splits the suite across jobs, each of which leaves a blob per phase that
// the run's last job merges into one HTML report and one JSON file
// (merge.config.ts). Anywhere else the suite writes both itself; the JSON
// feeds the gate's summary, a file per phase.
const REPORTS_DIR = "../reports";
const shardName = process.env.SHARD ? `-${process.env.SHARD.replace("/", "-of-")}` : "";
const reporter: ReporterDescription[] = process.env.CI
  ? [["list"], ["blob", { outputDir: `${REPORTS_DIR}/e2e-blob/${PHASE ?? "all"}`, fileName: `report-${PHASE ?? "all"}${shardName}.zip` }]]
  : [
      ["list"],
      ["html", { outputFolder: PHASE === "instance" ? "playwright-report-instance" : "playwright-report", open: "never" }],
      ["json", { outputFile: `${REPORTS_DIR}/e2e${PHASE ? `-${PHASE}` : ""}/e2e.json` }],
    ];

export default defineConfig({
  testDir: "./tests",
  outputDir: PHASE === "instance" ? "./test-results-instance" : "./test-results",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  timeout: 20_000,
  expect: { timeout: 5_000 },
  reporter,
  use: {
    baseURL: WEB_URL,
    // The interface follows the browser's language, and the specs read its
    // English words; a spec about another language says so itself.
    locale: "en-US",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    actionTimeout: 5_000,
    navigationTimeout: 10_000,
  },
  projects: [
    { name: "setup", testMatch: /.*\.setup\.ts/, teardown: "teardown" },
    { name: "teardown", testMatch: /.*\.teardown\.ts/ },
    ...(PHASE === "instance" ? [] : parallelProjects),
    ...(PHASE === "parallel" ? [] : instanceProjects),
  ],
});
