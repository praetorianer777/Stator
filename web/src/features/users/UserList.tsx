import { useMe, useMembers, useSetMemberActive, type Member } from "@/api/auth";
import { Button, EmptyState, ErrorBanner, Skeleton, Table, Tag, Td, Th, countOf, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

/** Every account of the installation, with whether it can sign in and a way to switch it off or back on. */
export function UserList() {
  const { data: me } = useMe();
  const { data, isLoading, error } = useMembers();
  const setActive = useSetMemberActive();
  const confirm = useConfirm();
  const toast = useToast();
  const members = data ?? [];
  const u = t.admin.users;

  async function switchOff(member: Member) {
    const name = member.name || member.email;
    const things = t.kit.confirm.things;
    if (!(await confirm({ noun: things.named(things.account, name), verb: t.kit.confirm.verbs.turnOff, body: u.switchOffBody(name) }))) return;
    setActive.mutate({ userId: member.userId, active: false }, { onSuccess: () => toast.success(u.switchedOff(name)) });
  }

  function switchOn(member: Member) {
    const name = member.name || member.email;
    setActive.mutate({ userId: member.userId, active: true }, { onSuccess: () => toast.success(u.switchedOn(name)) });
  }

  return (
    <section aria-labelledby="users-accounts" className="space-y-2" data-users="">
      <h2 id="users-accounts" className="text-sm font-semibold text-ink">
        {u.accounts}
      </h2>
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
      {setActive.error && <ErrorBanner>{setActive.error.message}</ErrorBanner>}
      {isLoading ? (
        <Skeleton lines={4} />
      ) : members.length === 0 ? (
        <EmptyState title={u.empty} />
      ) : (
        <>
          <p className="text-sm text-ink-muted" data-users-total={members.length}>
            {countOf(members.length, t.kit.counted.member)}
          </p>
          <Table dense aria-label={u.accounts}>
            <thead>
              <tr>
                <Th>{u.columnPerson}</Th>
                <Th>{u.columnState}</Th>
                <Th>
                  <span className="sr-only">{u.switchOff}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {members.map((member) => {
                const self = member.userId === me?.user.id;
                const name = member.name || member.email;
                return (
                  <tr key={member.userId} data-user={member.email} data-user-active={member.active}>
                    <Td className="min-w-0">
                      <div className="truncate text-ink">
                        {name}
                        {self && <span className="text-ink-subtle"> ({u.you})</span>}
                      </div>
                      <div className="truncate text-xs text-ink-subtle">{member.email}</div>
                    </Td>
                    <Td>{member.active ? <span className="text-ink-muted">{u.on}</span> : <Tag className="text-danger">{u.off}</Tag>}</Td>
                    <Td className="text-right">
                      {!self &&
                        (member.active ? (
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => void switchOff(member)}
                            aria-label={u.switchOffWho(name)}
                            data-action="switch-off"
                          >
                            {u.switchOff}
                          </Button>
                        ) : (
                          <Button size="sm" variant="secondary" onClick={() => switchOn(member)} aria-label={u.switchOnWho(name)} data-action="switch-on">
                            {u.switchOn}
                          </Button>
                        ))}
                    </Td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        </>
      )}
    </section>
  );
}
