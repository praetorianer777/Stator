import { useMeta } from "@/api/meta";
import { WEB_BUILD } from "@/config";
import { buildDetails, buildLabel } from "@/lib/build";

/** Which build is running: the api's, which is what a bug report needs, or this client's own while the api cannot say. */
export function BuildStamp({ className }: { className: string }) {
  const { data: meta } = useMeta();
  const build = meta?.build ?? WEB_BUILD;
  return (
    <span className={className} title={buildDetails(build)} data-build={meta ? "api" : "web"}>
      {buildLabel(build)}
    </span>
  );
}
