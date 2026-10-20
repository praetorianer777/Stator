-- +goose Up
-- Connecting a person to Armature at their sign-in (#430), by exchanging
-- their access token from the shared identity provider. A refused exchange
-- never fails the sign-in; Armature's sentence is kept here, so the profile
-- says why the person is not connected. Storing a grant or a token forgets
-- it, and it goes with the person's membership and with the connection.
CREATE TABLE armature_sign_in (
    user_id    uuid PRIMARY KEY REFERENCES member (user_id) ON DELETE CASCADE,
    connection boolean NOT NULL DEFAULT true CONSTRAINT armature_sign_in_connection_one CHECK (connection)
               REFERENCES armature_connection (singleton) ON DELETE CASCADE,
    refusal    text NOT NULL CONSTRAINT armature_sign_in_refusal_said CHECK (btrim(refusal) <> ''),
    refused_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE armature_sign_in ENABLE ROW LEVEL SECURITY;
ALTER TABLE armature_sign_in FORCE  ROW LEVEL SECURITY;
CREATE POLICY armature_sign_in_app ON armature_sign_in USING (true) WITH CHECK (true);
CREATE POLICY armature_sign_in_admin_bypass ON armature_sign_in TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY armature_sign_in_own ON armature_sign_in AS RESTRICTIVE FOR ALL TO stator_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id() AND perm_is_member(current_actor_id()));
-- An anonymous reader names nobody, so none of them is theirs.
CREATE POLICY armature_sign_in_not_anonymous ON armature_sign_in AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());

REVOKE ALL ON armature_sign_in FROM stator_app;
GRANT SELECT, DELETE ON armature_sign_in TO stator_app;
GRANT INSERT (user_id, refusal, refused_at) ON armature_sign_in TO stator_app;
GRANT UPDATE (refusal, refused_at) ON armature_sign_in TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON armature_sign_in TO stator_admin;

-- +goose Down
DROP TABLE IF EXISTS armature_sign_in;
