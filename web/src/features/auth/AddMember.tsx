import { useState, type FormEvent } from "react";
import { useCreateMember, type CreatedMember } from "@/api/auth";
import { Button, ErrorBanner, Field, Select } from "@/components/ui";
import { ApiError } from "@/api/client";
import { t } from "@/i18n";

type Role = "member" | "admin";

/**
 * Adds a person with a password, for an organization whose people do not all
 * come through an identity provider. A password left out is made, and shown
 * once here, since nothing keeps it.
 */
export function AddMember() {
  const create = useCreateMember();
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState<Role>("member");
  const [password, setPassword] = useState("");
  const [added, setAdded] = useState<CreatedMember | null>(null);
  const [copied, setCopied] = useState(false);
  const fields = create.error instanceof ApiError ? (create.error.fields ?? {}) : {};

  function reset() {
    setEmail("");
    setName("");
    setRole("member");
    setPassword("");
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { email: email.trim(), name: name.trim(), role, password },
      {
        onSuccess: (member) => {
          setAdded(member);
          setCopied(false);
          reset();
          setOpen(false);
        },
      },
    );
  }

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="space-y-3" data-add-member="">
      {!open && (
        <Button size="sm" onClick={() => setOpen(true)} data-action="add-member">
          {t.sso.addPerson}
        </Button>
      )}
      {open && (
        <form onSubmit={submit} className="space-y-3 rounded-control border border-border p-3" aria-label={t.sso.addPerson}>
          <p className="text-sm text-ink-muted">{t.sso.addPersonIntro}</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t.sso.addEmail} type="email" autoComplete="off" required value={email} onChange={(event) => setEmail(event.target.value)} error={fields.email} />
            <Field label={t.sso.addName} autoComplete="off" value={name} onChange={(event) => setName(event.target.value)} hint={t.sso.addNameHint} error={fields.name} />
            <Select label={t.sso.role} value={role} onChange={(event) => setRole(event.target.value as Role)} error={fields.role}>
              <option value="member">{t.sso.roleNames.member}</option>
              <option value="admin">{t.sso.roleNames.admin}</option>
            </Select>
            <Field
              label={t.sso.addPassword}
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              hint={t.sso.addPasswordHint}
              error={fields.password}
            />
          </div>
          {create.error && Object.keys(fields).length === 0 && <ErrorBanner>{create.error.message}</ErrorBanner>}
          <div className="flex gap-2">
            <Button type="submit" size="sm" loading={create.isPending} data-action="create-member">
              {t.sso.addPersonSubmit}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              onClick={() => {
                setOpen(false);
                reset();
                create.reset();
              }}
            >
              {t.sso.cancel}
            </Button>
          </div>
        </form>
      )}
      {added && (
        <div role="status" className="space-y-2 rounded-control border border-border p-3" data-member-added={added.email}>
          <p className="text-sm text-ink">{added.newAccount ? t.sso.addedNew(added.name || added.email) : t.sso.addedExisting(added.name || added.email)}</p>
          {added.password && (
            <div className="space-y-1">
              <p className="text-sm text-ink-muted">{t.sso.passwordOnce}</p>
              <div className="flex flex-wrap items-center gap-2">
                <code className="rounded-control bg-surface-sunken px-2 py-1 font-mono text-sm text-ink select-all" data-new-password="">
                  {added.password}
                </code>
                <Button size="sm" variant="secondary" onClick={() => copy(added.password!)} data-action="copy-password">
                  {copied ? t.sso.copied : t.sso.copyPassword}
                </Button>
              </div>
            </div>
          )}
          <Button size="sm" variant="secondary" onClick={() => setAdded(null)}>
            {t.sso.done}
          </Button>
        </div>
      )}
    </div>
  );
}
