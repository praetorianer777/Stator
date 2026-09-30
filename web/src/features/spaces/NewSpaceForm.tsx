import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { suggestKey, useCreateSpace } from "@/api/spaces";
import { useCanCreateSpace } from "@/features/permissions/access";
import { Button, ErrorBanner, Field, PageHeader } from "@/components/ui";
import { SPACE_DESCRIPTION_MAX_LENGTH, SPACE_KEY_MAX_LENGTH, SPACE_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

/** Makes a space; the key follows the name until somebody types one of their own. */
export function NewSpaceForm() {
  const navigate = useNavigate();
  const mayCreate = useCanCreateSpace();
  const create = useCreateSpace();
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [description, setDescription] = useState("");
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const shownKey = keyTouched ? key : suggestKey(name, SPACE_KEY_MAX_LENGTH);

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate({ name, key: shownKey, description }, { onSuccess: (made) => void navigate({ to: "/s/$spaceKey", params: { spaceKey: made.key } }) });
  }

  return (
    <div className="mx-auto max-w-2xl" data-new-space>
      <PageHeader crumbs={[{ label: t.spaces.title, render: (label) => <Link to="/spaces">{label}</Link> }]} title={t.spaces.createTitle} />
      {!mayCreate ? (
        <p className="text-sm text-ink-muted">{t.spaces.notAdmin}</p>
      ) : (
        <form onSubmit={submit} className="space-y-4" noValidate>
          {create.error && !Object.keys(fields).length && <ErrorBanner>{create.error.message}</ErrorBanner>}
          <Field
            label={t.spaces.name}
            hint={t.spaces.nameHint}
            value={name}
            maxLength={SPACE_NAME_MAX_LENGTH}
            onChange={(event) => setName(event.target.value)}
            error={fields.name}
            autoFocus
            required
          />
          <Field
            label={t.spaces.key}
            hint={t.spaces.keyHint}
            value={shownKey}
            maxLength={SPACE_KEY_MAX_LENGTH}
            className="font-mono uppercase"
            onChange={(event) => {
              setKeyTouched(true);
              setKey(event.target.value.toUpperCase());
            }}
            error={fields.key}
            required
          />
          <Field
            label={t.spaces.description}
            hint={t.spaces.descriptionHint}
            value={description}
            maxLength={SPACE_DESCRIPTION_MAX_LENGTH}
            rows={3}
            onChange={(event) => setDescription(event.target.value)}
            error={fields.description}
          />
          <div className="flex gap-2">
            <Button type="submit" loading={create.isPending} data-action="create-space">
              {t.spaces.submit}
            </Button>
            <Button type="button" variant="secondary" onClick={() => navigate({ to: "/spaces" })}>
              {t.spaces.cancel}
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}
