import { type BuildStamp, SHORT_COMMIT_LENGTH } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "./format";

// git describe's hash, which the label shows on its own after the dot.
const DESCRIBED_HASH = /\.g([0-9a-f]+)$/;

const builtAtFormat = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

function shortCommit(build: BuildStamp): string | undefined {
  const commit = build.commit || DESCRIBED_HASH.exec(build.version)?.[1];
  return commit?.slice(0, SHORT_COMMIT_LENGTH);
}

/** The build as the sidebar names it, always with its commit: v1.2.3 · abc1234, v1.2.3+4 · abc1234. */
export function buildLabel(build: BuildStamp): string {
  const version = build.version.replace(DESCRIBED_HASH, "");
  const named = /^\d/.test(version) ? `v${version}` : version;
  // Said rather than left out, so an image built without COMMIT shows it.
  return `${named} · ${shortCommit(build) ?? t.nav.buildCommitUnknown}`;
}

/** The whole stamp, one fact a line, for the label's tooltip and a bug report. */
export function buildDetails(build: BuildStamp): string {
  const lines = [t.nav.buildVersion(build.version)];
  const commit = shortCommit(build);
  if (commit) lines.push(t.nav.buildCommit(commit));
  const builtAt = build.builtAt ? new Date(build.builtAt) : null;
  if (builtAt && !Number.isNaN(builtAt.getTime())) lines.push(t.nav.buildBuiltAt(builtAtFormat.format(builtAt)));
  return lines.join("\n");
}
