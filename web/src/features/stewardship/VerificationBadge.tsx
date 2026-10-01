import { Icon } from "@/components/icons";
import { Tooltip, cx } from "@/components/ui";
import type { Verification } from "@/api/stewardship";
import { localDateFormat } from "@/lib/format";
import { t } from "@/i18n";

const day = localDateFormat({ dateStyle: "medium" });

type Status = Verification["status"];

/** The words and the glyph of a state; the tint comes from index.css by data-verification-badge. */
function Face({ status }: { status: Status }) {
  return (
    <>
      {status === "verified" ? <Icon.Seal /> : <Icon.Warning />}
      {status === "verified" ? t.stewardship.verified : t.stewardship.expired}
    </>
  );
}

const shape = "verification-badge inline-flex items-center gap-1 rounded-control font-medium whitespace-nowrap";

/** Says a page was checked and still holds, or that its check ran out, and opens who owns it and who verified it. */
export function VerificationBadge({ verification, onOpen }: { verification: Verification; onOpen: () => void }) {
  const until = day.format(new Date(verification.expiresAt));
  const why = verification.status === "verified" ? t.stewardship.badgeVerified(until) : t.stewardship.badgeExpired(until);
  return (
    <Tooltip text={why}>
      <button
        type="button"
        onClick={onOpen}
        aria-label={why}
        className={cx(shape, "h-6 px-1.5 text-2xs hover:underline")}
        data-verification-badge={verification.status}
      >
        <Face status={verification.status} />
      </button>
    </Tooltip>
  );
}

/** The small badge a list puts beside a verified page's title. */
export function VerifiedMark() {
  return (
    <span className={cx(shape, "h-5 px-1 text-2xs")} data-verification-badge="verified">
      <Face status="verified" />
    </span>
  );
}
