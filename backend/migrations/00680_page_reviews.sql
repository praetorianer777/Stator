-- +goose Up
-- Reviews and read confirmations (#313). A space, or a branch of its pages,
-- may hold its pages to a review by the people and groups a rule names: a
-- draft is then submitted as a snapshot, which the named reviewers approve
-- or send back, and the approval that completes the count publishes it as
-- its author's version. A page may also ask its readers to confirm they read
-- it from a version on. The database holds what the service promises, so a
-- request that skips the service is refused all the same.

-- A rule is the whole space's (space_id) or a branch's (page_id), never
-- both. A branch's rule hangs off its top page and follows it when it moves.
CREATE TABLE review_rule (
    org_id     uuid NOT NULL,
    id         uuid NOT NULL DEFAULT uuidv7(),
    space_id   uuid,
    page_id    uuid,
    required   integer NOT NULL,
    updated_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    FOREIGN KEY (org_id, space_id) REFERENCES space (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    CONSTRAINT review_rule_scope CHECK ((space_id IS NULL) <> (page_id IS NULL)),
    -- review.MaxRequired.
    CONSTRAINT review_rule_required CHECK (required BETWEEN 1 AND 10)
);
CREATE UNIQUE INDEX review_rule_space_idx ON review_rule (org_id, space_id) WHERE space_id IS NOT NULL;
CREATE UNIQUE INDEX review_rule_page_idx ON review_rule (org_id, page_id) WHERE page_id IS NOT NULL;

-- People and groups, never everyone: a review is somebody's judgement.
CREATE TABLE review_rule_subject (
    org_id       uuid NOT NULL,
    rule_id      uuid NOT NULL,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    user_id      uuid,
    group_id     uuid,
    CONSTRAINT review_rule_subject_whole CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL)),
    FOREIGN KEY (org_id, rule_id) REFERENCES review_rule (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, group_id) REFERENCES groups (org_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX review_rule_subject_idx
    ON review_rule_subject (org_id, rule_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;

-- A review is the draft as it was submitted, with the rule's reviewers and
-- count as they stood then, so changing the rule moves no goal posts.
CREATE TABLE page_review (
    org_id          uuid NOT NULL,
    id              uuid NOT NULL DEFAULT uuidv7(),
    page_id         uuid NOT NULL,
    author_id       uuid REFERENCES app_user(id) ON DELETE SET NULL,
    base_version    integer NOT NULL,
    title           text NOT NULL,
    body            jsonb NOT NULL,
    comment         text NOT NULL DEFAULT '',
    notify_watchers boolean NOT NULL DEFAULT false,
    required        integer NOT NULL,
    state           text NOT NULL DEFAULT 'pending',
    skip_reason     text,
    skipped_by      uuid REFERENCES app_user(id) ON DELETE SET NULL,
    version         integer,
    submitted_at    timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz,
    PRIMARY KEY (org_id, id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    CONSTRAINT page_review_state_known CHECK (state IN ('pending', 'changes_requested', 'withdrawn', 'published', 'skipped')),
    CONSTRAINT page_review_required CHECK (required BETWEEN 1 AND 10),
    -- page.MaxCommentLength.
    CONSTRAINT page_review_comment_length CHECK (char_length(comment) <= 500),
    -- review.MaxReasonLength.
    CONSTRAINT page_review_skip_reason CHECK (
        (state = 'skipped') = (skip_reason IS NOT NULL)
        AND (skip_reason IS NULL OR (btrim(skip_reason) <> '' AND char_length(skip_reason) <= 500))),
    CONSTRAINT page_review_version CHECK ((state IN ('published', 'skipped')) = (version IS NOT NULL)),
    CONSTRAINT page_review_finished CHECK ((state = 'pending') = (finished_at IS NULL))
);
CREATE UNIQUE INDEX page_review_pending_idx ON page_review (org_id, page_id) WHERE state = 'pending';
CREATE INDEX page_review_page_idx ON page_review (org_id, page_id, submitted_at DESC);

CREATE TABLE page_review_subject (
    org_id       uuid NOT NULL,
    review_id    uuid NOT NULL,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    user_id      uuid,
    group_id     uuid,
    CONSTRAINT page_review_subject_whole CHECK (
        (subject_type = 'user' AND user_id IS NOT NULL AND group_id IS NULL) OR
        (subject_type = 'group' AND group_id IS NOT NULL AND user_id IS NULL)),
    FOREIGN KEY (org_id, review_id) REFERENCES page_review (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, group_id) REFERENCES groups (org_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX page_review_subject_idx
    ON page_review_subject (org_id, review_id, subject_type, user_id, group_id) NULLS NOT DISTINCT;

-- One row per reviewer, rewritten by their next word: the last one counts.
CREATE TABLE page_review_decision (
    org_id      uuid NOT NULL,
    id          uuid NOT NULL DEFAULT uuidv7(),
    review_id   uuid NOT NULL,
    reviewer_id uuid REFERENCES app_user(id) ON DELETE SET NULL,
    decision    text NOT NULL CHECK (decision IN ('approve', 'changes')),
    comment     text NOT NULL DEFAULT '',
    decided_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, id),
    UNIQUE (org_id, review_id, reviewer_id),
    FOREIGN KEY (org_id, review_id) REFERENCES page_review (org_id, id) ON DELETE CASCADE,
    CONSTRAINT page_review_decision_says_why CHECK (decision = 'approve' OR btrim(comment) <> ''),
    -- review.MaxCommentLength.
    CONSTRAINT page_review_decision_comment_length CHECK (char_length(comment) <= 500)
);

-- The version a review went out as names it, for the history.
ALTER TABLE page_version ADD COLUMN review_id uuid;
ALTER TABLE page_version ADD CONSTRAINT page_version_review_fk
    FOREIGN KEY (org_id, review_id) REFERENCES page_review (org_id, id) ON DELETE SET NULL (review_id);

-- A page asking its readers to confirm they read it, from a version on.
CREATE TABLE page_read_ask (
    org_id   uuid NOT NULL,
    page_id  uuid NOT NULL,
    since    integer NOT NULL CHECK (since >= 1),
    asked_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    asked_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, page_id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE
);

CREATE TABLE page_read_confirmation (
    org_id       uuid NOT NULL,
    page_id      uuid NOT NULL,
    user_id      uuid NOT NULL,
    version      integer NOT NULL,
    confirmed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, page_id, user_id),
    FOREIGN KEY (org_id, page_id) REFERENCES page (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES org_member (org_id, user_id) ON DELETE CASCADE
);

-- +goose StatementBegin
-- The space a rule is about: its own, or the one its branch's top page is
-- in now, whoever may see that page.
CREATE FUNCTION review_rule_space(rule_space uuid, rule_page uuid) RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(rule_space, (SELECT p.space_id FROM page p WHERE p.org_id = current_org_id() AND p.id = rule_page))
$$;

CREATE FUNCTION review_rule_administrable(rule_space uuid, rule_page uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND COALESCE(perm_space_holds(actor, review_rule_space(rule_space, rule_page), 'administer'), false)
$$;

CREATE FUNCTION review_rule_viewable(rule_space uuid, rule_page uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT COALESCE(perm_space_holds(actor, review_rule_space(rule_space, rule_page), 'view'), false)
$$;

-- Whether a live page is in a space, or at or below a page, read past every
-- restriction: a rule may not cover one, whether its setter sees it or not.
CREATE FUNCTION page_live_within(within_space uuid, top uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    WITH RECURSIVE below (id) AS (
        SELECT p.id FROM page p WHERE p.org_id = current_org_id() AND top IS NOT NULL AND p.id = top
        UNION ALL
        SELECT p.id FROM page p JOIN below b ON p.parent_id = b.id WHERE p.org_id = current_org_id()
    )
    SELECT EXISTS (
        SELECT 1 FROM page p
        WHERE p.org_id = current_org_id() AND p.mode = 'live' AND p.trashed_at IS NULL
          AND ((top IS NULL AND p.space_id = within_space) OR p.id IN (SELECT id FROM below)))
$$;

CREATE FUNCTION review_rule_administrable_by_id(rule uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM review_rule r WHERE r.org_id = current_org_id() AND r.id = rule
                     AND review_rule_administrable(r.space_id, r.page_id, actor))
$$;

CREATE FUNCTION review_rule_viewable_by_id(rule uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM review_rule r WHERE r.org_id = current_org_id() AND r.id = rule
                     AND review_rule_viewable(r.space_id, r.page_id, actor))
$$;

-- Whether a review names the actor, directly or by a group they are in.
CREATE FUNCTION page_review_names(review uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM page_review_subject s
        WHERE s.org_id = current_org_id() AND s.review_id = review
          AND perm_subject_matches(actor, s.subject_type, s.user_id, s.group_id))
$$;

-- Whether the actor may decide on a review: named on it, not its author,
-- able to view its page, and only while it is pending.
CREATE FUNCTION page_review_decidable(review uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT actor IS NOT NULL AND EXISTS (
        SELECT 1 FROM page_review r
        WHERE r.org_id = current_org_id() AND r.id = review AND r.state = 'pending'
          AND r.author_id IS DISTINCT FROM actor
          AND perm_page_viewable(r.page_id, actor) AND page_review_names(review, actor))
$$;

CREATE FUNCTION page_review_viewable(review uuid, actor uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM page_review r WHERE r.org_id = current_org_id() AND r.id = review
                     AND perm_page_viewable(r.page_id, actor))
$$;

CREATE FUNCTION page_review_approvals(review uuid) RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT count(*)::integer FROM page_review_decision d
    WHERE d.org_id = current_org_id() AND d.review_id = review AND d.decision = 'approve'
$$;

-- The review waiting on a page, null when none is.
CREATE FUNCTION page_review_pending_on(target uuid) RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT r.id FROM page_review r WHERE r.org_id = current_org_id() AND r.page_id = target AND r.state = 'pending'
$$;

CREATE FUNCTION page_review_by(review uuid, author uuid) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM page_review r WHERE r.org_id = current_org_id() AND r.id = review AND r.author_id = author)
$$;

-- A review leaves pending once, by the way its state says: withdrawn by its
-- author, sent back by a reviewer who asked for changes, published with its
-- approvals in, or skipped by an administrator of the space with a reason;
-- either of the last two names the version it went out as.
CREATE FUNCTION page_review_guard() RETURNS trigger
    LANGUAGE plpgsql SET search_path = public, pg_temp
AS $$
DECLARE
    actor uuid := current_actor_id();
BEGIN
    IF current_user <> 'stator_app' THEN
        RETURN NEW;
    END IF;
    IF OLD.state <> 'pending' THEN
        RAISE EXCEPTION 'a review that was decided stays as it was decided' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'pending' THEN
        RETURN NEW;
    END IF;
    IF NEW.state = 'withdrawn' AND OLD.author_id IS DISTINCT FROM actor THEN
        RAISE EXCEPTION 'only its author withdraws a review' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'changes_requested' AND NOT EXISTS (
        SELECT 1 FROM page_review_decision d
        WHERE d.org_id = OLD.org_id AND d.review_id = OLD.id AND d.reviewer_id = actor AND d.decision = 'changes') THEN
        RAISE EXCEPTION 'only a reviewer who asked for changes sends a review back' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'published' AND page_review_approvals(OLD.id) < OLD.required THEN
        RAISE EXCEPTION 'a review is published only with its approvals' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state = 'skipped' AND (NEW.skipped_by IS DISTINCT FROM actor OR NOT EXISTS (
        SELECT 1 FROM page p WHERE p.org_id = OLD.org_id AND p.id = OLD.page_id
          AND perm_space_holds(actor, p.space_id, 'administer'))) THEN
        RAISE EXCEPTION 'only an administrator of the space publishes a review without its approvals' USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.state IN ('published', 'skipped') AND NOT EXISTS (
        SELECT 1 FROM page_version v
        WHERE v.org_id = OLD.org_id AND v.page_id = OLD.page_id AND v.number = NEW.version AND v.review_id = OLD.id) THEN
        RAISE EXCEPTION 'a review goes out as a version that names it' USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
END;
$$;

-- The version from which a page's readers confirm, its current one.
CREATE FUNCTION page_current_version(target uuid) RETURNS integer
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT p.version FROM page p WHERE p.org_id = current_org_id() AND p.id = target
$$;

-- Whether a page asks its readers to confirm a version: the current one,
-- since the ask began.
CREATE FUNCTION page_read_confirmable(target uuid, confirmed integer) RETURNS boolean
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp
AS $$
    SELECT EXISTS (SELECT 1 FROM page_read_ask a WHERE a.org_id = current_org_id() AND a.page_id = target AND a.since <= confirmed)
       AND confirmed = page_current_version(target)
$$;
-- +goose StatementEnd

CREATE TRIGGER page_review_guard BEFORE UPDATE ON page_review
    FOR EACH ROW EXECUTE FUNCTION page_review_guard();

ALTER TABLE review_rule ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_rule FORCE  ROW LEVEL SECURITY;
CREATE POLICY review_rule_tenant_isolation ON review_rule
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY review_rule_admin_bypass ON review_rule TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY review_rule_not_anonymous ON review_rule AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY review_rule_readers ON review_rule AS RESTRICTIVE FOR SELECT TO stator_app
    USING (review_rule_viewable(space_id, page_id, current_actor_id()));
CREATE POLICY review_rule_setters ON review_rule AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (review_rule_administrable(space_id, page_id, current_actor_id()));
CREATE POLICY review_rule_changers ON review_rule AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (review_rule_administrable(space_id, page_id, current_actor_id()))
    WITH CHECK (review_rule_administrable(space_id, page_id, current_actor_id()));
CREATE POLICY review_rule_removers ON review_rule AS RESTRICTIVE FOR DELETE TO stator_app
    USING (review_rule_administrable(space_id, page_id, current_actor_id()));
REVOKE ALL ON review_rule FROM stator_app;
GRANT SELECT, INSERT, DELETE ON review_rule TO stator_app;
GRANT UPDATE (required, updated_by, updated_at) ON review_rule TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON review_rule TO stator_admin;

ALTER TABLE review_rule_subject ENABLE ROW LEVEL SECURITY;
ALTER TABLE review_rule_subject FORCE  ROW LEVEL SECURITY;
CREATE POLICY review_rule_subject_tenant_isolation ON review_rule_subject
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY review_rule_subject_admin_bypass ON review_rule_subject TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY review_rule_subject_not_anonymous ON review_rule_subject AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY review_rule_subject_readers ON review_rule_subject AS RESTRICTIVE FOR SELECT TO stator_app
    USING (review_rule_viewable_by_id(rule_id, current_actor_id()));
CREATE POLICY review_rule_subject_setters ON review_rule_subject AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (review_rule_administrable_by_id(rule_id, current_actor_id()));
CREATE POLICY review_rule_subject_removers ON review_rule_subject AS RESTRICTIVE FOR DELETE TO stator_app
    USING (review_rule_administrable_by_id(rule_id, current_actor_id()));
REVOKE ALL ON review_rule_subject FROM stator_app;
GRANT SELECT, INSERT, DELETE ON review_rule_subject TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON review_rule_subject TO stator_admin;

ALTER TABLE page_review ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_review FORCE  ROW LEVEL SECURITY;
CREATE POLICY page_review_tenant_isolation ON page_review
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_review_admin_bypass ON page_review TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_review_not_anonymous ON page_review AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY page_review_readers ON page_review AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
-- Submitted by an editor of the page as its author, pending and undecided.
CREATE POLICY page_review_submitters ON page_review AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (author_id = current_actor_id() AND perm_page_editable(page_id, current_actor_id())
                AND state = 'pending' AND version IS NULL AND finished_at IS NULL
                AND skip_reason IS NULL AND skipped_by IS NULL);
-- Who may move it on, and how, is page_review_guard's.
CREATE POLICY page_review_deciders ON page_review AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()))
    WITH CHECK (perm_page_viewable(page_id, current_actor_id()));
REVOKE ALL ON page_review FROM stator_app;
GRANT SELECT, INSERT ON page_review TO stator_app;
GRANT UPDATE (state, skip_reason, skipped_by, version, finished_at) ON page_review TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_review TO stator_admin;

ALTER TABLE page_review_subject ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_review_subject FORCE  ROW LEVEL SECURITY;
CREATE POLICY page_review_subject_tenant_isolation ON page_review_subject
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_review_subject_admin_bypass ON page_review_subject TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_review_subject_not_anonymous ON page_review_subject AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY page_review_subject_readers ON page_review_subject AS RESTRICTIVE FOR SELECT TO stator_app
    USING (page_review_viewable(review_id, current_actor_id()));
-- Named once, by the author, while the review is theirs and pending.
CREATE POLICY page_review_subject_namers ON page_review_subject AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (page_review_by(review_id, current_actor_id()) AND page_review_pending_on(
        (SELECT r.page_id FROM page_review r WHERE r.org_id = current_org_id() AND r.id = review_id)) = review_id
        AND page_review_approvals(review_id) = 0);
REVOKE ALL ON page_review_subject FROM stator_app;
GRANT SELECT, INSERT ON page_review_subject TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_review_subject TO stator_admin;

ALTER TABLE page_review_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_review_decision FORCE  ROW LEVEL SECURITY;
CREATE POLICY page_review_decision_tenant_isolation ON page_review_decision
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_review_decision_admin_bypass ON page_review_decision TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_review_decision_not_anonymous ON page_review_decision AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY page_review_decision_readers ON page_review_decision AS RESTRICTIVE FOR SELECT TO stator_app
    USING (page_review_viewable(review_id, current_actor_id()));
CREATE POLICY page_review_decision_makers ON page_review_decision AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (reviewer_id = current_actor_id() AND page_review_decidable(review_id, current_actor_id()));
CREATE POLICY page_review_decision_changers ON page_review_decision AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (reviewer_id = current_actor_id() AND page_review_decidable(review_id, current_actor_id()))
    WITH CHECK (reviewer_id = current_actor_id() AND page_review_decidable(review_id, current_actor_id()));
REVOKE ALL ON page_review_decision FROM stator_app;
GRANT SELECT, INSERT ON page_review_decision TO stator_app;
GRANT UPDATE (decision, comment, decided_at) ON page_review_decision TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_review_decision TO stator_admin;

-- No version goes out around a pending review: while one waits, the only
-- version written is the one that names it, as its author's.
CREATE POLICY page_version_around_reviews ON page_version AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (review_id IS NOT DISTINCT FROM page_review_pending_on(page_id)
                AND (review_id IS NULL OR page_review_by(review_id, created_by)));

ALTER TABLE page_read_ask ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_read_ask FORCE  ROW LEVEL SECURITY;
CREATE POLICY page_read_ask_tenant_isolation ON page_read_ask
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_read_ask_admin_bypass ON page_read_ask TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_read_ask_not_anonymous ON page_read_ask AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
CREATE POLICY page_read_ask_readers ON page_read_ask AS RESTRICTIVE FOR SELECT TO stator_app
    USING (perm_page_viewable(page_id, current_actor_id()));
-- An editor asks from the version the page is at, in their own name.
CREATE POLICY page_read_ask_askers ON page_read_ask AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (asked_by = current_actor_id() AND perm_page_editable(page_id, current_actor_id())
                AND since = page_current_version(page_id));
CREATE POLICY page_read_ask_renewers ON page_read_ask AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (perm_page_editable(page_id, current_actor_id()))
    WITH CHECK (asked_by = current_actor_id() AND perm_page_editable(page_id, current_actor_id())
                AND since = page_current_version(page_id));
CREATE POLICY page_read_ask_stoppers ON page_read_ask AS RESTRICTIVE FOR DELETE TO stator_app
    USING (perm_page_editable(page_id, current_actor_id()));
REVOKE ALL ON page_read_ask FROM stator_app;
GRANT SELECT, INSERT, DELETE ON page_read_ask TO stator_app;
GRANT UPDATE (since, asked_by, asked_at) ON page_read_ask TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_read_ask TO stator_admin;

ALTER TABLE page_read_confirmation ENABLE ROW LEVEL SECURITY;
ALTER TABLE page_read_confirmation FORCE  ROW LEVEL SECURITY;
CREATE POLICY page_read_confirmation_tenant_isolation ON page_read_confirmation
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
CREATE POLICY page_read_confirmation_admin_bypass ON page_read_confirmation TO stator_admin USING (true) WITH CHECK (true);
CREATE POLICY page_read_confirmation_not_anonymous ON page_read_confirmation AS RESTRICTIVE FOR ALL TO stator_app
    USING (NOT current_anonymous()) WITH CHECK (NOT current_anonymous());
-- One's own, and everybody's for whoever sees who read the page.
CREATE POLICY page_read_confirmation_readers ON page_read_confirmation AS RESTRICTIVE FOR SELECT TO stator_app
    USING (user_id = current_actor_id() OR perm_page_readers_listable(page_id, current_actor_id()));
-- A reader confirms for themselves alone, a page they may view that asks,
-- as it is now.
CREATE POLICY page_read_confirmation_confirmers ON page_read_confirmation AS RESTRICTIVE FOR INSERT TO stator_app
    WITH CHECK (user_id = current_actor_id() AND perm_page_viewable(page_id, current_actor_id())
                AND page_read_confirmable(page_id, version));
CREATE POLICY page_read_confirmation_reconfirmers ON page_read_confirmation AS RESTRICTIVE FOR UPDATE TO stator_app
    USING (user_id = current_actor_id())
    WITH CHECK (user_id = current_actor_id() AND perm_page_viewable(page_id, current_actor_id())
                AND page_read_confirmable(page_id, version));
REVOKE ALL ON page_read_confirmation FROM stator_app;
GRANT SELECT, INSERT ON page_read_confirmation TO stator_app;
GRANT UPDATE (version, confirmed_at) ON page_read_confirmation TO stator_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON page_read_confirmation TO stator_admin;

-- A draft put to somebody to review, and the word on one's own draft. It
-- follows 00520, which last redefined the kinds, so it lists every one.
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('assigned', 'due', 'mentioned', 'shared', 'replied', 'commented', 'resolved', 'published', 'created', 'posted', 'expired', 'failed', 'review', 'reviewed'));

-- +goose Down
DELETE FROM notification WHERE kind IN ('review', 'reviewed');
ALTER TABLE notification DROP CONSTRAINT notification_kind_check;
ALTER TABLE notification ADD CONSTRAINT notification_kind_check
    CHECK (kind IN ('assigned', 'due', 'mentioned', 'shared', 'replied', 'commented', 'resolved', 'published', 'created', 'posted', 'expired', 'failed'));
DROP POLICY IF EXISTS page_version_around_reviews ON page_version;
ALTER TABLE page_version DROP CONSTRAINT IF EXISTS page_version_review_fk;
ALTER TABLE page_version DROP COLUMN IF EXISTS review_id;
DROP TABLE IF EXISTS page_read_confirmation;
DROP TABLE IF EXISTS page_read_ask;
DROP TABLE IF EXISTS page_review_decision;
DROP TABLE IF EXISTS page_review_subject;
DROP TRIGGER IF EXISTS page_review_guard ON page_review;
DROP TABLE IF EXISTS page_review;
DROP TABLE IF EXISTS review_rule_subject;
DROP TABLE IF EXISTS review_rule;
DROP FUNCTION IF EXISTS page_read_confirmable(uuid, integer);
DROP FUNCTION IF EXISTS page_current_version(uuid);
DROP FUNCTION IF EXISTS page_review_guard();
DROP FUNCTION IF EXISTS page_review_by(uuid, uuid);
DROP FUNCTION IF EXISTS page_review_pending_on(uuid);
DROP FUNCTION IF EXISTS page_review_approvals(uuid);
DROP FUNCTION IF EXISTS page_review_viewable(uuid, uuid);
DROP FUNCTION IF EXISTS page_review_decidable(uuid, uuid);
DROP FUNCTION IF EXISTS page_review_names(uuid, uuid);
DROP FUNCTION IF EXISTS review_rule_viewable_by_id(uuid, uuid);
DROP FUNCTION IF EXISTS review_rule_administrable_by_id(uuid, uuid);
DROP FUNCTION IF EXISTS review_rule_viewable(uuid, uuid, uuid);
DROP FUNCTION IF EXISTS review_rule_administrable(uuid, uuid, uuid);
DROP FUNCTION IF EXISTS page_live_within(uuid, uuid);
DROP FUNCTION IF EXISTS review_rule_space(uuid, uuid);
