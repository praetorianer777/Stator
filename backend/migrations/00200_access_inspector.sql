-- +goose Up
-- Inspecting somebody's access (#81) names the grants that bring them in.
-- The policy on global_grant shows its rows to organization administrators
-- alone, yet an administrator of a space may inspect who can do what there,
-- and use is part of that answer.

-- +goose StatementBegin
-- The global grants of a permission that reach the actor, matched the way
-- perm_global_grants matches them. Only somebody who may inspect anybody at
-- all is answered: the actor themselves, an organization administrator, or
-- an administrator of some space.
CREATE FUNCTION perm_global_grant_sources(actor uuid, permission text)
    RETURNS TABLE (subject_type text, user_id uuid, group_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT g.subject_type, g.user_id, g.group_id
    FROM global_grant g
    WHERE g.org_id = current_org_id() AND g.permission = perm_global_grant_sources.permission
      AND perm_is_member(actor)
      AND perm_subject_matches(actor, g.subject_type, g.user_id, g.group_id)
      AND (actor = current_actor_id() OR perm_is_admin(current_actor_id()) OR EXISTS (
          SELECT 1 FROM space s
          WHERE s.org_id = current_org_id() AND perm_space_holds(current_actor_id(), s.id, 'administer')))
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS perm_global_grant_sources(uuid, text);
