import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { INVITABLE_ROLES, useGuests, useInviteGuest, useRemoveGuest, type Guest, type InvitableRole } from "@/api/guests";
import type { Space } from "@/api/spaces";
import { Button, Card, ErrorBanner, Field, Select, Skeleton, Table, Td, Th } from "@/components/ui";
import { GUEST_EMAIL_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

/**
 * The people from outside whom the organization's administrators let into this
 * space alone: inviting one by address, and taking them out again.
 */
export function SpaceGuests({ space }: { space: Space }) {
  const { data: guests, error, refetch } = useGuests(space.key, true);
  const invite = useInviteGuest(space.key);
  const remove = useRemoveGuest(space.key);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<InvitableRole>("viewer");
  const [confirming, setConfirming] = useState<Guest | null>(null);
  const [notice, setNotice] = useState("");
  const fields = invite.error instanceof ApiError ? invite.error.fields : {};
  const failure = (invite.error && Object.keys(fields).length === 0 ? invite.error : null) ?? remove.error;

  function submit(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    invite.mutate(
      { email, role },
      {
        onSuccess: (made) => {
          setEmail("");
          setNotice(t.guests.invited(made.email));
        },
      },
    );
  }

  function removeConfirmed(guest: Guest) {
    setNotice("");
    remove.mutate(
      { userId: guest.userId },
      {
        onSuccess: () => {
          setConfirming(null);
          setNotice(t.guests.removed(guest.name || guest.email));
        },
      },
    );
  }

  return (
    <Card className="space-y-3 p-4" data-space-guests={space.key}>
      <h2 className="text-sm font-semibold text-ink">{t.guests.title}</h2>
      <p className="text-sm text-ink-muted">{t.guests.intro}</p>
      {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
      {!guests && !error && <Skeleton />}
      {guests?.length === 0 && <p className="text-sm text-ink-subtle">{t.guests.empty}</p>}
      {guests && guests.length > 0 && (
        <Table dense aria-label={t.guests.title}>
          <thead>
            <tr>
              <Th>{t.guests.columnGuest}</Th>
              <Th>{t.guests.columnRole}</Th>
              <Th>
                <span className="sr-only">{t.guests.remove}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {guests.map((guest) => {
              const name = guest.name || guest.email;
              return (
                <tr key={guest.userId} data-guest={guest.email}>
                  <Td className="min-w-0">
                    <div className="truncate text-ink">{name}</div>
                    <div className="truncate text-xs text-ink-subtle">{guest.email}</div>
                  </Td>
                  <Td className="text-ink" data-guest-role={guest.role}>
                    {t.guests.roleNames[guest.role]}
                  </Td>
                  <Td className="text-right">
                    <Button size="sm" variant="secondary" onClick={() => setConfirming(guest)} aria-label={t.guests.removeWho(name)} data-action="remove-guest">
                      {t.guests.remove}
                    </Button>
                  </Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}
      {confirming && (
        <div
          role="alertdialog"
          aria-labelledby="remove-guest-question"
          className="space-y-2 rounded-control border border-border p-3"
          data-confirm-remove-guest=""
        >
          <p id="remove-guest-question" className="text-sm text-ink">
            {t.guests.confirmRemove(confirming.name || confirming.email)}
          </p>
          <div className="flex gap-2">
            <Button size="sm" variant="danger" onClick={() => removeConfirmed(confirming)} loading={remove.isPending} data-action="confirm-remove-guest">
              {t.guests.confirm}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setConfirming(null)}>
              {t.guests.cancel}
            </Button>
          </div>
        </div>
      )}
      <form onSubmit={submit} noValidate aria-label={t.guests.inviteTitle} className="grid items-start gap-3 sm:grid-cols-[1fr_12rem_auto]">
        <Field
          label={t.guests.email}
          type="email"
          autoComplete="off"
          value={email}
          maxLength={GUEST_EMAIL_MAX_LENGTH}
          placeholder="ada@example.com"
          hint={t.guests.emailHint}
          error={fields.email}
          onChange={(event) => setEmail(event.target.value)}
        />
        <Select label={t.guests.role} value={role} error={fields.role} onChange={(event) => setRole(event.target.value as InvitableRole)}>
          {INVITABLE_ROLES.map((each) => (
            <option key={each} value={each}>
              {t.guests.roleNames[each]}
            </option>
          ))}
        </Select>
        <Button type="submit" className="sm:mt-6" loading={invite.isPending} data-action="invite-guest">
          {t.guests.invite}
        </Button>
      </form>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {notice && (
        <p role="status" className="text-sm text-ink-muted">
          {notice}
        </p>
      )}
    </Card>
  );
}
