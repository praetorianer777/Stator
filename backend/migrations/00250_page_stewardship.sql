-- +goose Up
-- Page owners and verification (#68). An owner is the one person who answers
-- for a page; a verification says somebody who may edit it checked it was
-- right, until a date. Neither is part of the page row: page_write_guard and
-- the published stamp would read every change to them as an edit.
CREATE TABLE page_owner (
    org_id  uuid NOT NULL,
    page_id uuid NOT NULL,
    user_id uuid NOT NULL,
    set_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    set_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, page_id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    -- An owner who leaves the organization leaves the page without one.
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE
);

CREATE INDEX page_owner_user_idx ON page_owner (org_id, user_id);

CREATE TABLE page_verification (
    org_id      uuid NOT NULL,
    page_id     uuid NOT NULL,
    verified_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    verified_at timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    -- The version the page was at when it was checked, so a reader can tell
    -- it changed since.
    version     integer NOT NULL CHECK (version > 0),
    -- Set by the worker once it has told the owner the verification lapsed,
    -- so a lapse is told once; a new verification clears it.
    lapse_noticed_at timestamptz,
    PRIMARY KEY (org_id, page_id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    CONSTRAINT page_verification_term CHECK (expires_at > verified_at AND expires_at <= verified_at + interval '730 days')
);

-- The worker looks for lapses nobody was told about yet, across organizations.
CREATE INDEX page_verification_due_idx ON page_verification (expires_at) WHERE lapse_noticed_at IS NULL;

-- +goose StatementBegin
-- Owners and verification are kept by whoever may edit the page, and only on
-- a published page out of the trash: an unpublished one has one reader.
CREATE FUNCTION page_stewardable(page uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM page p WHERE p.id = page AND p.org_id = current_org_id()
                     AND p.trashed_at IS NULL AND p.version > 0)
       AND perm_page_editable(page, actor)
$$;

-- When and at which version a page was verified are the database's to say,
-- so nobody can backdate a check or claim one of a version never read. The
-- worker's notice of a lapse changes nothing else and keeps them.
CREATE FUNCTION page_verification_stamp() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp
AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.lapse_noticed_at IS DISTINCT FROM OLD.lapse_noticed_at
       AND NEW.verified_by IS NOT DISTINCT FROM OLD.verified_by AND NEW.expires_at = OLD.expires_at THEN
        NEW.verified_at := OLD.verified_at;
        NEW.version := OLD.version;
        RETURN NEW;
    END IF;
    NEW.verified_at := now();
    NEW.lapse_noticed_at := NULL;
    NEW.version := (SELECT p.version FROM page p WHERE p.id = NEW.page_id AND p.org_id = NEW.org_id);
    RETURN NEW;
END;
$$;

CREATE FUNCTION page_owner_stamp() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    NEW.set_at := now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER page_verification_stamp BEFORE INSERT OR UPDATE ON page_verification
    FOR EACH ROW EXECUTE FUNCTION page_verification_stamp();
CREATE TRIGGER page_owner_stamp BEFORE INSERT OR UPDATE ON page_owner
    FOR EACH ROW EXECUTE FUNCTION page_owner_stamp();

ALTER TABLE page_owner        ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_owner        FORCE  ROW LEVEL SECURITY;
ALTER TABLE page_verification ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_verification FORCE  ROW LEVEL SECURITY;

CREATE POLICY page_owner_tenant_isolation ON page_owner
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_verification_tenant_isolation ON page_verification
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_owner_admin_bypass ON page_owner TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_verification_admin_bypass ON page_verification TO stator_admin USING (true) WITH CHECK (true);

-- Read with the page. Written by its editors, in their own name, and an owner
-- must be somebody who may view the page; one who loses access later is kept,
-- and the page tells its editors to name another.
CREATE POLICY page_owner_read ON page_owner AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY page_owner_insert ON page_owner AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (page_stewardable(page_id, current_actor_id()) AND set_by = current_actor_id()
                AND perm_page_viewable(page_id, user_id));
CREATE POLICY page_owner_update ON page_owner AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (page_stewardable(page_id, current_actor_id()))
    WITH CHECK (page_stewardable(page_id, current_actor_id()) AND set_by = current_actor_id()
                AND perm_page_viewable(page_id, user_id));
CREATE POLICY page_owner_delete ON page_owner AS RESTRICTIVE FOR DELETE TO stator_app
    USING (page_stewardable(page_id, current_actor_id()));

CREATE POLICY page_verification_read ON page_verification AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
CREATE POLICY page_verification_insert ON page_verification AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (page_stewardable(page_id, current_actor_id()) AND verified_by = current_actor_id());
CREATE POLICY page_verification_update ON page_verification AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (page_stewardable(page_id, current_actor_id()))
    WITH CHECK (page_stewardable(page_id, current_actor_id()) AND verified_by = current_actor_id());
CREATE POLICY page_verification_delete ON page_verification AS RESTRICTIVE FOR DELETE TO stator_app
    USING (page_stewardable(page_id, current_actor_id()));

-- Only the worker notes a lapse; the app role cannot name that column.
REVOKE ALL ON page_owner, page_verification FROM stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_owner, page_verification TO stator_admin;
GRANT SELECT, DELETE ON page_owner, page_verification TO stator_app;
GRANT INSERT (org_id, page_id, user_id, set_by), UPDATE (user_id, set_by) ON page_owner TO stator_app;
GRANT INSERT (org_id, page_id, verified_by, expires_at), UPDATE (verified_by, expires_at) ON page_verification TO stator_app;

REVOKE EXECUTE ON FUNCTION page_stewardable(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION page_stewardable(uuid, uuid) TO stator_app, stator_admin;

-- A lapse tells the owner, as a kind of its own.
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('mentioned', 'replied', 'commented', 'resolved', 'published', 'created', 'expired'));

-- +goose Down
DELETE FROM notification WHERE kind = 'expired';
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('mentioned', 'replied', 'commented', 'resolved', 'published', 'created'));
DROP TABLE IF EXISTS page_verification;
DROP TABLE IF EXISTS page_owner;
DROP FUNCTION IF EXISTS page_owner_stamp();
DROP FUNCTION IF EXISTS page_verification_stamp();
DROP FUNCTION IF EXISTS page_stewardable(uuid, uuid);
