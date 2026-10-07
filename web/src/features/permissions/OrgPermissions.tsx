import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { subjectKey, subjectRef, useOrgPermissions, useSetOrgPermission, type GlobalGrant, type Subject } from "@/api/permissions";
import { Button, Card, ErrorBanner, PageHeader, Skeleton } from "@/components/ui";
import { OrgAnonymousAccess, OrgPublicLinks } from "@/features/public/AnonymousAccess";
import { t } from "@/i18n";
import { SubjectList } from "./SubjectList";
import { SubjectPicker } from "./SubjectPicker";

/** The organization's global permissions, one card each; a card's changes wait for its own save. */
export function OrgPermissions() {
  const { data, error, refetch, isLoading } = useOrgPermissions();
  return (
    <div className="mx-auto max-w-3xl" data-org-permissions>
      <PageHeader crumb={t.settings.title} title={t.permissions.orgTitle} />
      <p className="mb-4 text-sm text-ink-muted">{t.permissions.orgIntro}</p>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {isLoading && <Skeleton />}
      <div className="space-y-4">
        {(data ?? []).map((grant) => (
          <GrantCard key={grant.permission} grant={grant} />
        ))}
        <OrgAnonymousAccess />
        <OrgPublicLinks />
      </div>
    </div>
  );
}

const keys = (subjects: Subject[]) => subjects.map(subjectKey).join(",");

function GrantCard({ grant }: { grant: GlobalGrant }) {
  const { data: me } = useMe();
  const save = useSetOrgPermission();
  const [subjects, setSubjects] = useState<Subject[]>(grant.subjects);
  const [notice, setNotice] = useState("");
  const name = t.permissions.globalNames[grant.permission];
  const dirty = keys(subjects) !== keys(grant.subjects);

  const change = (next: Subject[]) => {
    setNotice("");
    save.reset();
    setSubjects(next);
  };

  return (
    <Card className="space-y-3 p-4" data-global-permission={grant.permission}>
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-sm font-semibold text-ink">{name}</h2>
        <span className="font-mono text-2xs text-ink-subtle">{grant.permission}</span>
      </div>
      <p className="text-sm text-ink-muted">{t.permissions.globalHints[grant.permission]}</p>
      <SubjectList
        subjects={subjects}
        empty={t.permissions.globalEmpty}
        selfId={me?.user.id}
        onRemove={grant.fixed ? undefined : (gone) => change(subjects.filter((each) => subjectKey(each) !== subjectKey(gone)))}
      />
      {grant.fixed ? (
        <p className="text-sm text-ink-subtle" data-fixed>
          {t.permissions.fixedNote}{" "}
          <Link to="/settings/sso" className="text-accent underline underline-offset-2">
            {t.permissions.fixedLink}
          </Link>
        </p>
      ) : (
        <>
          <SubjectPicker
            label={t.permissions.pickerLabel}
            allowEveryone
            exclude={subjects.map(subjectKey)}
            onPick={(picked) => {
              change([...subjects, picked]);
              setNotice(t.permissions.added(picked.name));
            }}
          />
          {save.error && <ErrorBanner>{save.error.message}</ErrorBanner>}
          <div className="flex flex-wrap items-center gap-2">
            <Button
              disabled={!dirty}
              loading={save.isPending}
              onClick={() =>
                save.mutate(
                  { permission: grant.permission, subjects: subjects.map(subjectRef) },
                  {
                    onSuccess: (saved) => {
                      setSubjects(saved.subjects);
                      setNotice(t.permissions.saved);
                    },
                  },
                )
              }
              data-action="save-global"
            >
              {t.permissions.save}
            </Button>
            {dirty && (
              <Button variant="secondary" onClick={() => change(grant.subjects)} data-action="discard-global">
                {t.permissions.discard}
              </Button>
            )}
            <span role="status" className="text-sm text-ink-muted">
              {notice || (dirty ? t.permissions.unsaved : "")}
            </span>
          </div>
        </>
      )}
    </Card>
  );
}
