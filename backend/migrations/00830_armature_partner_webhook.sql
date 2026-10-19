-- +goose Up
-- Connecting Armature as an administrator in one step (#429): Stator makes
-- its own webhook in Armature through the connected app, and keeps the
-- secret Armature answers as the pasted one is kept.

-- webhook_partner says the stored secret is of the webhook Stator made
-- itself, so a new address or a disconnect removes it there first.
-- webhook_refusal is Armature's sentence when it would not make it, such as
-- its outbound guard refusing Stator's address, for the settings page.
ALTER TABLE armature_connection
    ADD COLUMN webhook_partner boolean NOT NULL DEFAULT false,
    ADD COLUMN webhook_refusal text,
    ADD CONSTRAINT armature_connection_partner_has_secret CHECK (NOT webhook_partner OR webhook_secret IS NOT NULL);

GRANT INSERT (webhook_partner, webhook_refusal) ON armature_connection TO stator_app;
GRANT UPDATE (webhook_partner, webhook_refusal) ON armature_connection TO stator_app;

-- A trip to the consent page connects the person's account, or, for an
-- administrator, also makes Stator's webhook with the grant it brings back.
ALTER TABLE armature_oauth_attempt
    ADD COLUMN purpose text NOT NULL DEFAULT 'account'
        CONSTRAINT armature_oauth_attempt_purpose_known CHECK (purpose IN ('account', 'webhook'));

-- Only an administrator starts the trip that writes the organization's
-- webhook secret on its way back.
CREATE POLICY armature_oauth_attempt_webhook_admins ON armature_oauth_attempt AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (purpose = 'account' OR perm_is_admin(current_actor_id()));

-- A secret Stator's own webhook was made with belongs to the Armature at
-- the old address, so a new one forgets it with the tokens; a pasted secret
-- stays, as it did.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION armature_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_token;
        DELETE FROM armature_app;
        NEW.armature_org_id := NULL;
        IF OLD.webhook_partner AND NEW.webhook_secret IS NOT DISTINCT FROM OLD.webhook_secret THEN
            NEW.webhook_secret := NULL;
            NEW.webhook_partner := false;
        END IF;
        NEW.webhook_refusal := NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION armature_connection_moved() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF NEW.base_url IS DISTINCT FROM OLD.base_url OR NEW.org_slug IS DISTINCT FROM OLD.org_slug THEN
        DELETE FROM armature_token;
        DELETE FROM armature_app;
        NEW.armature_org_id := NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
DROP POLICY IF EXISTS armature_oauth_attempt_webhook_admins ON armature_oauth_attempt;
ALTER TABLE armature_oauth_attempt DROP COLUMN IF EXISTS purpose;
REVOKE INSERT (webhook_partner, webhook_refusal), UPDATE (webhook_partner, webhook_refusal) ON armature_connection FROM stator_app;
ALTER TABLE armature_connection
    DROP CONSTRAINT IF EXISTS armature_connection_partner_has_secret,
    DROP COLUMN IF EXISTS webhook_refusal,
    DROP COLUMN IF EXISTS webhook_partner;
