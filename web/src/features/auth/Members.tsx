import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMe, useMembers, useRemoveMember, type Member } from "@/api/auth";
import { Button, Card, ErrorBanner, Table, Tag, Td, Th } from "@/components/ui";
import { t } from "@/i18n";
import { AddMember } from "./AddMember";

/**
 * The organization's people and their roles, marking the ones the identity
 * provider's groups decide, so an administrator knows which to change there.
 */
export function Members() {
  const { data: me } = useMe();
  const { data } = useMembers();
  const remove = useRemoveMember();
  const [confirming, setConfirming] = useState<Member | null>(null);
  const [notice, setNotice] = useState("");
  const members = data ?? [];
  if (members.length === 0) return null;

  function removeConfirmed(member: Member) {
    remove.mutate(
      { userId: member.userId },
      {
        onSuccess: () => {
          setConfirming(null);
          setNotice(t.sso.removed(member.name || member.email));
        },
      },
    );
  }

  return (
    <Card className="mt-4 space-y-3 p-4" data-members="">
      <h2 className="text-sm font-semibold text-ink">{t.sso.membersTitle}</h2>
      <p className="text-sm text-ink-muted">{t.sso.membersIntro}</p>
      <AddMember />
      <Table dense aria-label={t.sso.membersTitle}>
        <thead>
          <tr>
            <Th>{t.sso.columnPerson}</Th>
            <Th>{t.sso.columnRole}</Th>
            <Th>
              <span className="sr-only">{t.sso.remove}</span>
            </Th>
          </tr>
        </thead>
        <tbody>
          {members.map((member) => {
            const self = member.userId === me?.user.id;
            const name = member.name || member.email;
            return (
              <tr key={member.userId} data-member={member.email}>
                <Td className="min-w-0">
                  <div className="truncate text-ink">
                    {name}
                    {self && <span className="text-ink-subtle"> ({t.sso.you})</span>}
                  </div>
                  <div className="truncate text-xs text-ink-subtle">{member.email}</div>
                </Td>
                <Td>
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-ink" data-member-role={member.role}>
                      {t.sso.roleNames[member.role]}
                    </span>
                    {member.roleSource === "oidc" && <Tag data-role-source="oidc">{t.sso.fromProvider}</Tag>}
                    {member.guestSpace && (
                      <Link
                        to="/s/$spaceKey/settings"
                        params={{ spaceKey: member.guestSpace.key }}
                        search={{ tab: "guests" }}
                        data-guest-space={member.guestSpace.key}
                      >
                        <Tag>{t.guests.guestOf(member.guestSpace.name)}</Tag>
                      </Link>
                    )}
                  </div>
                </Td>
                <Td className="text-right">
                  {member.role !== "owner" && !self && (
                    <Button size="sm" variant="secondary" onClick={() => setConfirming(member)} aria-label={t.sso.removeWho(name)} data-action="remove-member">
                      {t.sso.remove}
                    </Button>
                  )}
                </Td>
              </tr>
            );
          })}
        </tbody>
      </Table>
      {confirming && (
        <div role="alertdialog" aria-labelledby="remove-member-question" className="space-y-2 rounded-control border border-border p-3" data-confirm-remove="">
          <p id="remove-member-question" className="text-sm text-ink">
            {t.sso.confirmRemove(confirming.name || confirming.email)}
          </p>
          <div className="flex gap-2">
            <Button size="sm" variant="danger" onClick={() => removeConfirmed(confirming)} loading={remove.isPending} data-action="confirm-remove-member">
              {t.sso.confirm}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setConfirming(null)}>
              {t.sso.cancel}
            </Button>
          </div>
        </div>
      )}
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      {notice && (
        <p role="status" className="text-sm text-ink-muted">
          {notice}
        </p>
      )}
    </Card>
  );
}
