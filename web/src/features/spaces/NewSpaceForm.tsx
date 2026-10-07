import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { ApiError } from "@/api/client";
import { suggestKey, useCreateSpace } from "@/api/spaces";
import { useCanCreateSpace } from "@/features/permissions/access";
import { Button, ErrorBanner, Field, PageHeader } from "@/components/ui";
import { BLANK_SPACE_TEMPLATE, SPACE_DESCRIPTION_MAX_LENGTH, SPACE_KEY_MAX_LENGTH, SPACE_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { SpaceTemplatePicker } from "./SpaceTemplatePicker";

/**
 * Makes a space, blank or from a space template; the key follows the name until somebody types
 * one of their own. A personal one, which everybody may make, starts blank and named for its owner.
 */
export function NewSpaceForm({ personal = false }: { personal?: boolean }) {
  const navigate = useNavigate();
  const mayCreateAny = useCanCreateSpace();
  const mayCreate = personal || mayCreateAny;
  const { data: me } = useMe();
  const create = useCreateSpace();
  const [name, setName] = useState("");
  const [nameTouched, setNameTouched] = useState(false);
  const [key, setKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [description, setDescription] = useState("");
  const [template, setTemplate] = useState(BLANK_SPACE_TEMPLATE);
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const shownName = nameTouched || !personal || !me ? name : t.spaces.personalName(me.user.name);
  // A personal space's key comes from its owner's name, not the possessive around it.
  const keySource = personal && !nameTouched && me ? me.user.name : shownName;
  const shownKey = keyTouched ? key : suggestKey(keySource, SPACE_KEY_MAX_LENGTH);

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      {
        name: shownName,
        key: shownKey,
        description,
        personal: personal || undefined,
        template: personal || template === BLANK_SPACE_TEMPLATE ? undefined : template,
      },
      { onSuccess: (made) => void navigate({ to: "/s/$spaceKey", params: { spaceKey: made.key } }) },
    );
  }

  return (
    <div className="mx-auto max-w-2xl" data-new-space={personal ? "personal" : ""}>
      <PageHeader
        crumbs={[{ label: t.spaces.title, render: (label) => <Link to="/spaces">{label}</Link> }]}
        title={personal ? t.spaces.createPersonalTitle : t.spaces.createTitle}
      />
      {personal && <p className="mb-4 text-sm text-ink-muted">{t.spaces.personalIntro}</p>}
      {!mayCreate ? (
        <p className="text-sm text-ink-muted">{t.spaces.notAdmin}</p>
      ) : (
        <form onSubmit={submit} className="space-y-4" noValidate>
          {create.error && !Object.keys(fields).length && <ErrorBanner>{create.error.message}</ErrorBanner>}
          <Field
            label={t.spaces.name}
            hint={t.spaces.nameHint}
            value={shownName}
            maxLength={SPACE_NAME_MAX_LENGTH}
            onChange={(event) => {
              setNameTouched(true);
              setName(event.target.value);
            }}
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
          {!personal && <SpaceTemplatePicker value={template} onChange={setTemplate} error={fields.template} />}
          <div className="flex gap-2">
            <Button type="submit" loading={create.isPending} data-action="create-space">
              {personal ? t.spaces.createPersonal : t.spaces.submit}
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
