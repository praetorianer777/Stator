import { defineConfig } from "@playwright/test";

// For `make e2e-merge-reports`, which joins the blobs of CI's browser jobs:
// the report a local run writes, as one report for the whole run.
export default defineConfig({
  testDir: "./tests",
  reporter: [
    ["html", { outputFolder: "playwright-report", open: "never" }],
    ["json", { outputFile: "../reports/e2e.json" }],
  ],
});
