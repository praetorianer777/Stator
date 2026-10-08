import { defineConfig, devices, type ReporterDescription } from "@playwright/test";
import { WEB_URL } from "./fixtures/stack";

// A spec that only means something at one width carries @desktop or @mobile
// and runs in that project alone; everything else runs in both.
const MOBILE_VIEWPORT = { width: 360, height: 740 };

// CI splits the suite across jobs, each of which leaves a blob that the run's
// last job merges into one HTML report and one JSON file (merge.config.ts).
// Anywhere else the suite writes both itself; the JSON feeds the gate's summary.
const REPORTS_DIR = "../reports";
const reporter: ReporterDescription[] = process.env.CI
  ? [["list"], ["blob", { outputDir: `${REPORTS_DIR}/e2e-blob` }]]
  : [["list"], ["html", { outputFolder: "playwright-report", open: "never" }], ["json", { outputFile: `${REPORTS_DIR}/e2e.json` }]];

export default defineConfig({
  testDir: "./tests",
  outputDir: "./test-results",
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
    {
      name: "chromium",
      dependencies: ["setup"],
      grepInvert: /@mobile/,
      use: { ...devices["Desktop Chrome"] },
    },
    {
      name: "mobile",
      dependencies: ["setup"],
      grepInvert: /@desktop/,
      use: { ...devices["Desktop Chrome"], viewport: MOBILE_VIEWPORT, isMobile: true, hasTouch: true },
    },
  ],
});
