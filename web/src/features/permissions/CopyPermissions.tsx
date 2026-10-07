import { useState } from "react";
import { ApiError } from "@/api/client";
import {
  useCopyPermissions,
  usePermissionCopyPreview,
  type CopySubject,
  type PermissionCopyChange,
  type PermissionCopyMode,
  type PermissionCopyPreview,
  type SpacePermission,
} from "@/api/permissions";
import { useSpaces, type Space } from "@/api/spaces";
import { Button, Dialog, ErrorBanner, Segmented, Select, Skeleton, Table, Tag, Td, Th } from "@/components/ui";
import { t } from "@/i18n";
import { SubjectGlyph } from "./SubjectPicker";

const MODES: PermissionCopyMode[] = ["merge", "replace"];

/** Offers copying another space's permissions onto this one, previewed before anything changes. */
export function CopyPermissions({ space, blocked, onCopied }: { space: Space; blocked: boolean; onCopied: (from: string) => void }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="space-y-2" data-permission-copy>
      <p className="text-sm text-ink-muted">{t.permissionCopy.intro}</p>
      <Button variant="secondary" disabled={blocked} onClick={() => setOpen(true)} data-action="copy-permissions">
        {t.permissionCopy.open}
      </Button>
      {blocked && <p className="text-sm text-ink-subtle">{t.permissionCopy.saveFirst}</p>}
      {open && (
        <CopyDialog
          space={space}
          onClose={() => setOpen(false)}
          onCopied={(from) => {
            setOpen(false);
            onCopied(from);
          }}
        />
      )}
    </div>
  );
}

function CopyDialog({ space, onClose, onCopied }: { space: Space; onClose: () => void; onCopied: (from: string) => void }) {
  const spaces = useSpaces();
  const [from, setFrom] = useState("");
  const [mode, setMode] = useState<PermissionCopyMode>("merge");
  const preview = usePermissionCopyPreview(space.key, from, mode);
  const copy = useCopyPermissions(space.key);
  // Only what the caller administers can be read to copy, and a personal space is its owner's own sharing.
  const sources = (spaces.data ?? []).filter((each) => each.key !== space.key && !each.owner && each.can.administer);
  const shown = preview.data;
  const canApply = Boolean(shown) && !preview.isFetching && !shown?.leavesNoAdministrator && (shown?.changes.length ?? 0) > 0;

  function apply() {
    if (!shown) return;
    copy.mutate(shown, {
      onSuccess: () => onCopied(shown.source.name),
      // A preview that went stale is shown afresh, so what is applied next is what the person sees.
      onError: (error) => {
        if (error instanceof ApiError && error.code === "copy_changed") void preview.refetch();
      },
    });
  }

  return (
    <Dialog title={t.permissionCopy.title(space.name)} wide onClose={onClose} data-permission-copy-dialog="">
      <div className="space-y-4">
        {spaces.error && <ErrorBanner onRetry={() => void spaces.refetch()}>{spaces.error.message}</ErrorBanner>}
        {spaces.data && sources.length === 0 ? (
          <p className="text-sm text-ink-muted">{t.permissionCopy.noSources}</p>
        ) : (
          <Select
            label={t.permissionCopy.source}
            value={from}
            onChange={(event) => {
              copy.reset();
              setFrom(event.target.value);
            }}
            data-copy-source=""
          >
            <option value="">{t.permissionCopy.sourcePlaceholder}</option>
            {sources.map((each) => (
              <option key={each.key} value={each.key}>
                {`${each.name} (${each.key})`}
              </option>
            ))}
          </Select>
        )}
        <div className="space-y-1">
          <p className="text-sm font-medium text-ink">{t.permissionCopy.mode}</p>
          <Segmented<PermissionCopyMode>
            label={t.permissionCopy.mode}
            value={mode}
            onChange={(next) => {
              copy.reset();
              setMode(next);
            }}
            options={MODES.map((each) => ({ value: each, label: t.permissionCopy.modes[each], attrs: { "data-copy-mode": each } }))}
          />
          <p className="text-sm text-ink-muted">{t.permissionCopy.modeHints[mode]}</p>
        </div>
        {from && <Preview query={preview} />}
        {copy.error && <ErrorBanner>{copy.error.message}</ErrorBanner>}
        <div className="flex flex-wrap items-center gap-2">
          <Button disabled={!canApply} loading={copy.isPending} onClick={apply} data-action="apply-permission-copy">
            {t.permissionCopy.apply}
          </Button>
          <Button variant="secondary" onClick={onClose}>
            {t.permissionCopy.cancel}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

function Preview({ query }: { query: ReturnType<typeof usePermissionCopyPreview> }) {
  if (query.error) return <ErrorBanner onRetry={() => void query.refetch()}>{query.error.message}</ErrorBanner>;
  const preview = query.data;
  if (!preview)
    return (
      <div>
        <p role="status" className="sr-only">
          {t.permissionCopy.loading}
        </p>
        <Skeleton lines={2} />
      </div>
    );
  return (
    <div className="space-y-3" data-copy-preview={preview.fingerprint}>
      {preview.changes.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.permissionCopy.noChanges}</p>
      ) : (
        <>
          <p className="text-sm text-ink-muted">{t.permissionCopy.summary(preview.changes.length, preview.counts.unchanged)}</p>
          <ChangeTable preview={preview} />
        </>
      )}
      {preview.leavesNoAdministrator && (
        <p role="alert" className="rounded-control border border-danger/30 bg-danger-subtle px-3 py-2 text-sm text-danger" data-copy-no-administrator>
          {t.permissionCopy.noAdministrator}
        </p>
      )}
      {preview.skipped.length > 0 && (
        <Notes title={t.permissionCopy.skippedTitle} lines={preview.skipped.map((each) => t.permissionCopy.skippedGuest(each.subject.name))} kind="skipped" />
      )}
      {preview.kept.length > 0 && (
        <Notes title={t.permissionCopy.keptTitle} lines={preview.kept.map((each) => t.permissionCopy.keptGuest(each.subject.name))} kind="kept" />
      )}
      <p className="text-sm text-ink-subtle">{t.permissionCopy.restrictionsNote}</p>
    </div>
  );
}

function ChangeTable({ preview }: { preview: PermissionCopyPreview }) {
  return (
    <Table dense aria-label={t.permissionCopy.table}>
      <thead>
        <tr>
          <Th>{t.permissionCopy.columnWho}</Th>
          <Th>{t.permissionCopy.columnBefore}</Th>
          <Th>{t.permissionCopy.columnAfter}</Th>
        </tr>
      </thead>
      <tbody>
        {preview.changes.map((change) => (
          <ChangeRow key={`${change.subject.type}:${change.subject.id ?? ""}`} change={change} />
        ))}
      </tbody>
    </Table>
  );
}

function ChangeRow({ change }: { change: PermissionCopyChange }) {
  const name = subjectName(change.subject);
  return (
    <tr data-copy-change={name} data-copy-kind={change.kind}>
      <Td className="align-top">
        <span className="flex items-center gap-2 text-ink">
          <SubjectGlyph type={change.subject.type === "anonymous" ? "everyone" : change.subject.type} />
          <span className="min-w-0 break-words">{name}</span>
        </span>
        <span className="mt-1 flex flex-wrap gap-1">
          <Tag className={change.kind === "removed" || change.kind === "narrowed" ? "text-danger" : undefined} title={t.permissionCopy.columnChange}>
            {t.permissionCopy.kinds[change.kind]}
          </Tag>
          {change.subject.guest && <Tag>{t.permissionCopy.guest}</Tag>}
        </span>
      </Td>
      <Td className="align-top text-ink-muted">
        <PermissionList permissions={change.before} />
      </Td>
      <Td className="align-top">
        <PermissionList permissions={change.after} />
      </Td>
    </tr>
  );
}

/** One permission a line, so a narrow screen wraps between them rather than scrolling the table. */
function PermissionList({ permissions }: { permissions: SpacePermission[] }) {
  if (permissions.length === 0) return <span>{t.permissionCopy.nothing}</span>;
  return (
    <ul>
      {permissions.map((each) => (
        <li key={each}>{t.permissions.spaceNames[each]}</li>
      ))}
    </ul>
  );
}

function Notes({ title, lines, kind }: { title: string; lines: string[]; kind: string }) {
  return (
    <div className="space-y-1" data-copy-notes={kind}>
      <p className="text-sm font-medium text-ink">{title}</p>
      <ul className="list-disc space-y-0.5 pl-5 text-sm text-ink-muted">
        {lines.map((line) => (
          <li key={line}>{line}</li>
        ))}
      </ul>
    </div>
  );
}

function subjectName(subject: CopySubject): string {
  if (subject.type === "everyone") return t.permissions.everyone;
  if (subject.type === "anonymous") return t.permissionCopy.anonymous;
  return subject.name;
}
