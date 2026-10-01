import { useId, useState } from "react";
import type { Page } from "@/api/pages";
import { ApiError } from "@/api/client";
import { subjectKey } from "@/api/permissions";
import { useSetOwner, useVerify } from "@/api/stewardship";
import { Button, Dialog, ErrorBanner, Select } from "@/components/ui";
import { Icon } from "@/components/icons";
import { VERIFY_DEFAULT_DAYS, VERIFY_TERM_DAYS } from "@/config";
import { SubjectPicker } from "@/features/permissions/SubjectPicker";
import { localDateFormat } from "@/lib/format";
import { t } from "@/i18n";

const day = localDateFormat({ dateStyle: "medium" });

/** The API's sentence about the one field this dialog sends, else its message. */
function refusal(error: Error, field: string): string {
  return (error instanceof ApiError && error.fields[field]) || error.message;
}

/**
 * Who answers for a page and whether it was checked. Everybody who may read
 * the page sees both; people who may edit it change them here.
 */
export function StewardshipDialog({ page, onClose }: { page: Page; onClose: () => void }) {
  const editable = page.can.edit && !page.unpublished;
  return (
    <Dialog title={t.stewardship.title(page.title)} onClose={onClose} data-stewardship-dialog={page.id}>
      <div className="space-y-5">
        <OwnerSection page={page} editable={editable} />
        <VerificationSection page={page} editable={editable} />
        {!editable && <p className="text-xs text-ink-subtle">{t.stewardship.readOnly}</p>}
      </div>
    </Dialog>
  );
}

function OwnerSection({ page, editable }: { page: Page; editable: boolean }) {
  const setOwner = useSetOwner(page.id);
  const heading = useId();
  const owner = page.owner;
  return (
    <section className="space-y-2" aria-labelledby={heading} data-owner-section>
      <h3 id={heading} className="text-sm font-semibold text-ink">
        {t.stewardship.ownerTitle}
      </h3>
      <p className="text-sm text-ink-muted">{t.stewardship.ownerIntro}</p>
      {owner ? (
        <p className="flex items-center gap-2 text-sm text-ink" data-owner={owner.name}>
          <Icon.User className="shrink-0 text-ink-subtle" />
          {owner.name}
        </p>
      ) : (
        <p className="text-sm text-ink-subtle" data-no-owner="">
          {t.stewardship.noOwner}
        </p>
      )}
      {owner && !owner.canView && (
        <p className="text-sm text-danger" data-owner-no-access="">
          {t.stewardship.ownerLostAccess(owner.name)}
        </p>
      )}
      {editable && (
        <div className="space-y-2">
          <SubjectPicker
            label={t.stewardship.pickOwner}
            peopleOnly
            exclude={owner ? [subjectKey({ type: "user", id: owner.id })] : []}
            disabled={setOwner.isPending}
            onPick={(subject) => {
              if (subject.type === "user" && subject.id) setOwner.mutate(subject.id);
            }}
          />
          {owner && (
            <Button variant="secondary" size="sm" loading={setOwner.isPending} onClick={() => setOwner.mutate(null)} data-action="remove-owner">
              {t.stewardship.removeOwner}
            </Button>
          )}
          {setOwner.error && <ErrorBanner>{refusal(setOwner.error, "userId")}</ErrorBanner>}
        </div>
      )}
    </section>
  );
}

function VerificationSection({ page, editable }: { page: Page; editable: boolean }) {
  const verify = useVerify(page.id);
  const [days, setDays] = useState<number>(VERIFY_DEFAULT_DAYS);
  const heading = useId();
  const v = page.verification;
  const who = v?.verifiedByName || t.stewardship.somebody;
  return (
    <section className="space-y-2" aria-labelledby={heading} data-verification-section>
      <h3 id={heading} className="text-sm font-semibold text-ink">
        {t.stewardship.verificationTitle}
      </h3>
      <p className="text-sm text-ink-muted">{t.stewardship.verificationIntro}</p>
      {v ? (
        <div className="space-y-1 text-sm text-ink" data-verification-state={v.status}>
          <p>
            {v.status === "verified"
              ? t.stewardship.verifiedBy(who, day.format(new Date(v.verifiedAt)), day.format(new Date(v.expiresAt)))
              : t.stewardship.expiredBy(who, day.format(new Date(v.verifiedAt)), day.format(new Date(v.expiresAt)))}
          </p>
          {v.version < page.version && (
            <p className="text-ink-muted" data-changed-since="">
              {t.stewardship.changedSince(v.version, page.version)}
            </p>
          )}
        </div>
      ) : (
        <p className="text-sm text-ink-subtle" data-not-verified="">
          {t.stewardship.notVerified}
        </p>
      )}
      {editable && (
        <div className="flex flex-wrap items-end gap-2">
          <Select label={t.stewardship.term} value={days} onChange={(event) => setDays(Number(event.target.value))} className="w-40" data-verify-term="">
            {VERIFY_TERM_DAYS.map((n) => (
              <option key={n} value={n}>
                {t.stewardship.days(n)}
              </option>
            ))}
          </Select>
          <Button icon={<Icon.Seal />} loading={verify.isPending && verify.variables !== null} onClick={() => verify.mutate(days)} data-action="verify-page">
            {v ? t.stewardship.verifyAgain : t.stewardship.verify}
          </Button>
          {v && (
            <Button variant="secondary" loading={verify.isPending && verify.variables === null} onClick={() => verify.mutate(null)} data-action="unverify-page">
              {t.stewardship.unverify}
            </Button>
          )}
        </div>
      )}
      {verify.error && <ErrorBanner>{refusal(verify.error, "days")}</ErrorBanner>}
    </section>
  );
}
