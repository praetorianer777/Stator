import { Link } from "@tanstack/react-router";
import { useMe, useMembers, useRemoveMember, type Member } from "@/api/auth";
import { Button, EmptyState, ErrorBanner, Skeleton, Table, Tag, Td, Th, countOf, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

/**
 * Who is in the installation, with their standing and the roles they hold
 * across it, by name or through a group. Letting people in is under Users.
 */
export function MemberList() {
  const { data: me } = useMe();
  const { data, isLoading, error } = useMembers();
  const remove = useRemoveMember();
  const confirm = useConfirm();
  const toast = useToast();
  const members = data ?? [];

  async function take(member: Member) {
    const name = member.name || member.email;
    if (await confirm({ noun: t.kit.confirm.things.member, verb: t.kit.confirm.verbs.remove, body: t.accessAdmin.members.removeBody(name) })) {
      remove.mutate({ userId: member.userId }, { onSuccess: () => toast.success(t.accessAdmin.members.removed(name)) });
    }
  }

  return (
    <div className="space-y-4" data-members="">
      <p className="text-sm text-ink-muted">{t.accessAdmin.members.intro}</p>
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
      {isLoading ? (
        <Skeleton lines={4} />
      ) : members.length === 0 ? (
        <EmptyState title={t.accessAdmin.members.empty} />
      ) : (
        <section className="space-y-2">
          <p className="text-sm text-ink-muted" data-members-total={members.length}>
            {countOf(members.length, t.kit.counted.member)}
          </p>
          <Table dense aria-label={t.accessAdmin.members.title}>
            <thead>
              <tr>
                <Th>{t.accessAdmin.members.columnPerson}</Th>
                <Th>{t.accessAdmin.members.columnStanding}</Th>
                <Th>{t.accessAdmin.members.columnRoles}</Th>
                <Th>
                  <span className="sr-only">{t.accessAdmin.members.remove}</span>
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
                        {self && <span className="text-ink-subtle"> ({t.accessAdmin.members.you})</span>}
                      </div>
                      <div className="truncate text-xs text-ink-subtle">{member.email}</div>
                    </Td>
                    <Td>
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="text-ink" data-member-role={member.role}>
                          {t.accessAdmin.members.standing[member.role] ?? member.role}
                        </span>
                        {!member.active && <Tag className="text-danger">{t.accessAdmin.members.off}</Tag>}
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
                    <Td>
                      {member.roles.length === 0 ? (
                        <span className="text-ink-subtle">{t.accessAdmin.members.noRoles}</span>
                      ) : (
                        <ul className="flex flex-wrap gap-1.5" data-member-roles>
                          {member.roles.map((role) => (
                            <li key={`${role.role}:${role.groupId ?? ""}`} data-member-holds={role.role}>
                              <Tag>
                                {role.name}
                                {role.groupName && (
                                  <span className="text-ink-subtle">
                                    {" "}
                                    {t.accessAdmin.members.through(role.groupBuiltin === "everyone" ? t.permissions.everyone : role.groupName)}
                                  </span>
                                )}
                              </Tag>
                            </li>
                          ))}
                        </ul>
                      )}
                    </Td>
                    <Td className="text-right">
                      {!self && (
                        <Button
                          size="sm"
                          variant="secondary"
                          onClick={() => void take(member)}
                          aria-label={t.accessAdmin.members.removeWho(name)}
                          data-action="remove-member"
                        >
                          {t.accessAdmin.members.remove}
                        </Button>
                      )}
                    </Td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        </section>
      )}
    </div>
  );
}
