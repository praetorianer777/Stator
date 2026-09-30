import { useState } from "react";
import { useMe } from "@/api/auth";
import { SPACE_PERMISSIONS, subjectKey, useSetSpacePermissions, useSpacePermissions, type SpaceGrant, type SpacePermission } from "@/api/permissions";
import type { Space } from "@/api/spaces";
import { Button, ErrorBanner, IconButton, Skeleton, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { SubjectGlyph, SubjectPicker } from "./SubjectPicker";

/** The permissions a set of ticks amounts to: administering holds all, and anything held holds viewing. */
export function impliedPermissions(held: SpacePermission[]): SpacePermission[] {
  if (held.includes("administer")) return [...SPACE_PERMISSIONS];
  if (held.length > 0 && !held.includes("view")) return SPACE_PERMISSIONS.filter((each) => each === "view" || held.includes(each));
  return SPACE_PERMISSIONS.filter((each) => held.includes(each));
}

/** Whether a permission is ticked only because a stronger one in the row is. */
function impliedBy(held: SpacePermission[], permission: SpacePermission): boolean {
  if (permission === "administer") return false;
  if (held.includes("administer")) return true;
  return permission === "view" && held.some((each) => each !== "view");
}

const shape = (grants: SpaceGrant[]) => JSON.stringify(grants.map((grant) => [subjectKey(grant.subject), [...grant.permissions].sort()]));

/** Who may do what in a space, as a grid of people and groups against permissions; nothing is sent until Save. */
export function SpacePermissions({ space }: { space: Space }) {
  const allowed = space.can.administer;
  const { data, error, refetch } = useSpacePermissions(space.key, allowed);
  if (!allowed) return <p className="text-sm text-ink-muted">{t.permissions.notSpaceAdmin}</p>;
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data) return <Skeleton />;
  return <Grid key={space.key} space={space} saved={data} />;
}

function Grid({ space, saved }: { space: Space; saved: SpaceGrant[] }) {
  const { data: me } = useMe();
  const save = useSetSpacePermissions(space.key);
  const [grants, setGrants] = useState<SpaceGrant[]>(saved);
  const [notice, setNotice] = useState("");
  const dirty = shape(grants) !== shape(saved);
  const hasEmpty = grants.some((grant) => grant.permissions.length === 0);

  const change = (next: SpaceGrant[]) => {
    setNotice("");
    save.reset();
    setGrants(next);
  };

  const toggle = (index: number, permission: SpacePermission, on: boolean) =>
    change(
      grants.map((grant, i) => {
        if (i !== index) return grant;
        const held = on ? [...grant.permissions, permission] : grant.permissions.filter((each) => each !== permission);
        return { ...grant, permissions: impliedPermissions(held) };
      }),
    );

  function submit() {
    // A row with nothing ticked grants nothing, which is what leaving it out says.
    save.mutate(
      grants.filter((grant) => grant.permissions.length > 0),
      {
        onSuccess: (stored) => {
          setGrants(stored);
          setNotice(t.permissions.saved);
        },
      },
    );
  }

  return (
    <div className="space-y-4" data-space-permissions>
      <p className="text-sm text-ink-muted">{t.permissions.spaceIntro}</p>
      {grants.length === 0 ? (
        <p className="text-sm text-ink-subtle">{t.permissions.spaceEmpty}</p>
      ) : (
        <Table dense aria-label={t.permissions.spaceGrid}>
          <thead>
            <tr>
              <Th>{t.permissions.columnWho}</Th>
              {SPACE_PERMISSIONS.map((permission) => (
                <Th key={permission} className="text-center" title={t.permissions.spaceHints[permission]} data-permission-column={permission}>
                  {t.permissions.spaceNames[permission]}
                </Th>
              ))}
              <Th>
                <span className="sr-only">{t.permissions.removeColumn}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {grants.map((grant, index) => {
              const who =
                grant.subject.type === "user" && grant.subject.id === me?.user.id ? `${grant.subject.name} (${t.permissions.you})` : grant.subject.name;
              return (
                <tr key={subjectKey(grant.subject)} data-grant={grant.subject.name} data-grant-type={grant.subject.type}>
                  <Td>
                    <span className="flex items-center gap-2 text-ink">
                      <SubjectGlyph type={grant.subject.type} />
                      {who}
                    </span>
                  </Td>
                  {SPACE_PERMISSIONS.map((permission) => {
                    const implied = impliedBy(grant.permissions, permission);
                    return (
                      <Td key={permission} className="text-center">
                        <input
                          type="checkbox"
                          className="size-4 rounded-[4px] border-border-strong accent-accent"
                          checked={grant.permissions.includes(permission)}
                          disabled={implied || save.isPending}
                          title={implied ? t.permissions.implied : undefined}
                          aria-label={t.permissions.cell(grant.subject.name, t.permissions.spaceNames[permission])}
                          onChange={(event) => toggle(index, permission, event.target.checked)}
                          data-permission-cell={permission}
                        />
                      </Td>
                    );
                  })}
                  <Td className="text-right">
                    <IconButton
                      icon={<Icon.X />}
                      label={t.permissions.remove(grant.subject.name)}
                      size="sm"
                      onClick={() => change(grants.filter((_, i) => i !== index))}
                      data-action="remove-grant"
                    />
                  </Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}
      <dl className="grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[auto_1fr]">
        {SPACE_PERMISSIONS.map((permission) => (
          <div key={permission} className="contents">
            <dt className="font-medium text-ink">{t.permissions.spaceNames[permission]}</dt>
            <dd className="text-ink-muted">{t.permissions.spaceHints[permission]}</dd>
          </div>
        ))}
      </dl>
      <SubjectPicker
        label={t.permissions.pickerLabel}
        allowEveryone
        exclude={grants.map((grant) => subjectKey(grant.subject))}
        onPick={(subject) => {
          change([...grants, { subject, permissions: ["view"] }]);
          setNotice(t.permissions.added(subject.name));
        }}
      />
      {hasEmpty && <p className="text-sm text-ink-subtle">{t.permissions.rowDropped}</p>}
      {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <div className="flex flex-wrap items-center gap-2">
        <Button disabled={!dirty} loading={save.isPending} onClick={submit} data-action="save-space-permissions">
          {t.permissions.save}
        </Button>
        {dirty && (
          <Button variant="secondary" onClick={() => change(saved)} data-action="discard-space-permissions">
            {t.permissions.discard}
          </Button>
        )}
        <span role="status" className="text-sm text-ink-muted">
          {notice || (dirty ? t.permissions.unsaved : "")}
        </span>
      </div>
    </div>
  );
}
