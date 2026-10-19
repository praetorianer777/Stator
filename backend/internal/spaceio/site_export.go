package spaceio

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// siteSnapshot is everything of an organization that is not a space, read in
// one transaction; each space is read in a transaction of its own after it.
type siteSnapshot struct {
	m        *SiteManifest
	spaces   []uuid.UUID
	logoKey  string
	themeKey map[uuid.UUID]string
	// total is the pages and files to write, for the progress.
	total int
}

// notCarried is what an organization's archive leaves out, and why, in the
// order the manifest and the documentation list it.
var notCarried = []Omission{
	{Key: "passwords", What: "Passwords", Why: "Only a bootstrap administrator has one, and it comes from the settings of the Stator it lands in."},
	{Key: "sessions", What: "Sessions", Why: "Everybody signs in again where the organization lands; their accounts are found by their address."},
	{Key: "tokens", What: "Personal access tokens", Why: "Their secrets are not kept in a readable form. Their owners make new ones."},
	{Key: "armatureAccounts", What: "Linked Armature accounts", Why: "Each person's Armature token is sealed with this server's key. Everybody connects their account again under their profile."},
	{Key: "signInSecret", What: "The sign-in provider's client secret", Why: "It is sealed with this server's key. The provider comes across switched off unless the organization it lands in has one; enter the secret and switch it on."},
	{Key: "webhookSecrets", What: "Webhook signing secrets", Why: "They are sealed with this server's key. Each webhook comes across switched off; rotate its secret, give the receiver the new one and switch it on."},
	{Key: "armatureWebhookSecret", What: "Armature's webhook secret", Why: "It is sealed with this server's key. Save the Armature connection again and enter the new secret in Armature."},
	{Key: "exciterAccounts", What: "Linked Exciter accounts", Why: "Each person's Exciter token is sealed with this server's key. Everybody pastes an Exciter token again under their profile."},
	{Key: "exciterWebhookSecret", What: "Exciter's webhook secret", Why: "It is sealed with this server's key. Save the Exciter connection again and enter the new secret in Exciter."},
	{Key: "joinRequests", What: "Requests to join", Why: "They wait for an administrator of this organization; whoever still wants in signs in again."},
	{Key: "links", What: "Shares, public links and anonymous reads by link", Why: "A link opens the organization to people; give them again where it lands."},
	{Key: "personal", What: "Notifications, stars, watches, page views and visits", Why: "They are each person's own reading of the old place."},
	{Key: "arrangements", What: "Owners, verifications, scheduled publishes, shortcuts, reviews and read confirmations of pages", Why: "They are arrangements rather than content, as in a space's archive."},
	{Key: "sharedDrafts", What: "A page's shared draft while people edit it together", Why: "Each person's own draft comes across, saved from it."},
	{Key: "trash", What: "The trash", Why: "What was deleted stays deleted."},
	{Key: "jobs", What: "Jobs and their files: exports, imports, Word imports and the example space's", Why: "They belong to the old place's worker."},
	{Key: "armatureThemes", What: "Themes mirrored from Armature", Why: "They come back from Armature when their owner follows Armature's theme again."},
	{Key: "address", What: "The organization's address (its slug)", Why: "The organization keeps the address of the Stator it lands in."},
}

// readSite reads the organization's own data, as the worker's role, and every
// count the archive's list of what it leaves out names.
func readSite(ctx context.Context, tx db.DBTX, exporter uuid.UUID) (*siteSnapshot, error) {
	s := &siteSnapshot{themeKey: map[uuid.UUID]string{}}
	m := &SiteManifest{Format: SiteFormat, Version: SiteVersion, ExportedBy: exporter.String(),
		People: []SitePerson{}, Groups: []SiteGroup{}, Permissions: []Grant{}, Themes: []SiteTheme{}, PersonThemes: []PersonTheme{},
		Templates: []Template{}, Webhooks: []SiteWebhook{}, Preferences: []SitePreference{}, Spaces: []SiteSpace{}}
	s.m = m
	if err := tx.QueryRow(ctx, `
		SELECT now(), name, slug::text, created_at, anonymous_access, anonymous_indexable, public_links, hub_page_id, hub_landing, default_theme_id
		FROM site`).Scan(&m.ExportedAt, &m.Organization.Name, &m.Organization.Slug, &m.Organization.CreatedAt,
		&m.Settings.AnonymousAccess, &m.Settings.AnonymousIndexable, &m.Settings.PublicLinks, &m.Settings.HubPage, &m.Settings.HubLanding,
		&m.Settings.DefaultTheme); err != nil {
		return nil, fmt.Errorf("read the organization: %w", err)
	}
	steps := []func() error{
		func() error { return s.readPeople(ctx, tx) },
		func() error { return s.readGroups(ctx, tx) },
		func() error { return s.readPermissions(ctx, tx) },
		func() error { return s.readSignIn(ctx, tx) },
		func() error { return s.readArmature(ctx, tx) },
		func() error { return s.readExciter(ctx, tx) },
		func() error { return s.readBrand(ctx, tx) },
		func() error { return s.readThemes(ctx, tx) },
		func() error { return s.readTemplates(ctx, tx) },
		func() error { return s.readWebhooks(ctx, tx) },
		func() error { return s.readPreferences(ctx, tx) },
		func() error { return s.readSpaces(ctx, tx) },
		func() error { return s.readNotCarried(ctx, tx) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *siteSnapshot) readPeople(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT u.id, u.name, u.email::text, u.timezone, u.locale, u.is_active, u.show_in_readers, m.role, m.role_source,
		       COALESCE(s.key, ''), m.created_at
		FROM member m JOIN app_user u ON u.id = m.user_id LEFT JOIN space s ON s.id = m.guest_space_id
		ORDER BY lower(u.email::text), u.id`)
	if err != nil {
		return err
	}
	s.m.People, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (SitePerson, error) {
		var (
			p  SitePerson
			id uuid.UUID
		)
		err := row.Scan(&id, &p.Name, &p.Email, &p.Timezone, &p.Locale, &p.Active, &p.ShowInReaders, &p.Role, &p.RoleSource, &p.GuestSpace, &p.JoinedAt)
		p.Ref = id.String()
		return p, err
	})
	if s.m.People == nil {
		s.m.People = []SitePerson{}
	}
	return err
}

func (s *siteSnapshot) readGroups(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT g.id, g.name, g.description, g.source, g.external_ref,
		       COALESCE(array_agg(m.user_id ORDER BY m.user_id) FILTER (WHERE m.user_id IS NOT NULL), '{}')
		FROM groups g LEFT JOIN group_member m ON m.group_id = g.id
		GROUP BY g.id ORDER BY lower(g.name), g.id`)
	if err != nil {
		return err
	}
	s.m.Groups, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (SiteGroup, error) {
		var (
			g       SiteGroup
			id      uuid.UUID
			members []uuid.UUID
		)
		err := row.Scan(&id, &g.Name, &g.Description, &g.Source, &g.ExternalRef, &members)
		g.Ref, g.Members = id.String(), make([]string, 0, len(members))
		for _, m := range members {
			g.Members = append(g.Members, m.String())
		}
		return g, err
	})
	if s.m.Groups == nil {
		s.m.Groups = []SiteGroup{}
	}
	return err
}

func (s *siteSnapshot) readPermissions(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT permission, subject_type, user_id, group_id FROM global_grant
		ORDER BY permission, subject_type, user_id, group_id`)
	if err != nil {
		return err
	}
	s.m.Permissions, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Grant, error) {
		var (
			g           Grant
			user, group *uuid.UUID
		)
		err := row.Scan(&g.Permission, &g.Subject.Type, &user, &group)
		g.Subject.Person, g.Subject.Group = idText(user), idText(group)
		return g, err
	})
	if s.m.Permissions == nil {
		s.m.Permissions = []Grant{}
	}
	return err
}

func (s *siteSnapshot) readSignIn(ctx context.Context, tx db.DBTX) error {
	var p SiteSignIn
	err := tx.QueryRow(ctx, `
		SELECT issuer, client_id, groups_claim, scopes, create_groups, enabled FROM oidc_provider`).
		Scan(&p.Issuer, &p.ClientID, &p.GroupsClaim, &p.Scopes, &p.CreateGroups, &p.Enabled)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		p = SiteSignIn{}
	case err != nil:
		return err
	}
	rows, err := tx.Query(ctx, `SELECT group_ref, role FROM oidc_group_role ORDER BY group_ref`)
	if err != nil {
		return err
	}
	p.GroupRoles, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (SiteGroupRole, error) {
		var r SiteGroupRole
		return r, row.Scan(&r.Group, &r.Role)
	})
	if err != nil {
		return err
	}
	if p.GroupRoles == nil {
		p.GroupRoles = []SiteGroupRole{}
	}
	if p.Issuer != "" || len(p.GroupRoles) > 0 {
		s.m.SignIn = &p
	}
	return nil
}

func (s *siteSnapshot) readArmature(ctx context.Context, tx db.DBTX) error {
	var a SiteArmature
	err := tx.QueryRow(ctx, `SELECT base_url FROM armature_connection`).Scan(&a.BaseURL)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return err
	}
	s.m.Armature = &a
	return nil
}

func (s *siteSnapshot) readExciter(ctx context.Context, tx db.DBTX) error {
	var e SiteExciter
	err := tx.QueryRow(ctx, `SELECT base_url FROM exciter_connection`).Scan(&e.BaseURL)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return err
	}
	s.m.Exciter = &e
	return nil
}

func (s *siteSnapshot) readBrand(ctx context.Context, tx db.DBTX) error {
	var (
		kind *string
		size *int64
		key  *string
	)
	err := tx.QueryRow(ctx, `SELECT footer_en, footer_de, logo_type, logo_size, logo_key FROM brand`).
		Scan(&s.m.Brand.FooterEn, &s.m.Brand.FooterDe, &kind, &size, &key)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return err
	}
	if kind != nil && size != nil && key != nil {
		s.m.Brand.Logo = &SiteLogo{ContentType: *kind, Size: *size}
		s.logoKey = *key
	}
	return nil
}

// readThemes reads the organization's themes and their files; a theme
// mirrored from Armature is its owner's copy of Armature's and stays behind.
func (s *siteSnapshot) readThemes(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT id, name, owner_id, shared, spec FROM theme WHERE armature_theme_id IS NULL ORDER BY lower(name), id`)
	if err != nil {
		return err
	}
	s.m.Themes, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (SiteTheme, error) {
		var (
			t     SiteTheme
			owner uuid.UUID
		)
		err := row.Scan(&t.ID, &t.Name, &owner, &t.Shared, &t.Spec)
		t.Owner, t.Files = owner.String(), []SiteThemeFile{}
		return t, err
	})
	if err != nil {
		return err
	}
	if s.m.Themes == nil {
		s.m.Themes = []SiteTheme{}
	}
	byID := map[uuid.UUID]*SiteTheme{}
	for i := range s.m.Themes {
		byID[s.m.Themes[i].ID] = &s.m.Themes[i]
	}
	rows, err = tx.Query(ctx, `SELECT id, theme_id, name, content_type, size, object_key FROM theme_asset ORDER BY theme_id, created_at, id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			f       SiteThemeFile
			themeID uuid.UUID
			key     string
		)
		if err := rows.Scan(&f.ID, &themeID, &f.Name, &f.ContentType, &f.Size, &key); err != nil {
			rows.Close()
			return err
		}
		if t := byID[themeID]; t != nil {
			t.Files = append(t.Files, f)
			s.themeKey[f.ID] = key
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `
		SELECT user_id, theme_id, follow_armature FROM user_theme u
		WHERE (theme_id IS NULL OR EXISTS (SELECT 1 FROM theme t WHERE t.id = u.theme_id AND t.armature_theme_id IS NULL))
		ORDER BY user_id`)
	if err != nil {
		return err
	}
	s.m.PersonThemes, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (PersonTheme, error) {
		var (
			p    PersonTheme
			user uuid.UUID
		)
		err := row.Scan(&user, &p.Theme, &p.FollowArmature)
		p.Person = user.String()
		return p, err
	})
	if s.m.PersonThemes == nil {
		s.m.PersonThemes = []PersonTheme{}
	}
	return err
}

func (s *siteSnapshot) readTemplates(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT name, description, title, body, variables FROM page_template WHERE space_id IS NULL ORDER BY lower(name), id`)
	if err != nil {
		return err
	}
	s.m.Templates, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Template, error) {
		var t Template
		return t, row.Scan(&t.Name, &t.Description, &t.Title, &t.Body, &t.Variables)
	})
	if s.m.Templates == nil {
		s.m.Templates = []Template{}
	}
	return err
}

func (s *siteSnapshot) readWebhooks(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT name, url, topics, enabled, owner_id, version FROM webhook_endpoint ORDER BY lower(name), id`)
	if err != nil {
		return err
	}
	s.m.Webhooks, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (SiteWebhook, error) {
		var (
			h     SiteWebhook
			owner *uuid.UUID
		)
		err := row.Scan(&h.Name, &h.URL, &h.Topics, &h.Enabled, &owner, &h.Version)
		h.Owner = idText(owner)
		return h, err
	})
	if s.m.Webhooks == nil {
		s.m.Webhooks = []SiteWebhook{}
	}
	return err
}

func (s *siteSnapshot) readPreferences(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `
		SELECT user_id, in_app, email, digest, auto_watch FROM notification_preference ORDER BY user_id`)
	if err != nil {
		return err
	}
	s.m.Preferences, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (SitePreference, error) {
		var (
			p    SitePreference
			user uuid.UUID
		)
		err := row.Scan(&user, &p.InApp, &p.Email, &p.Digest, &p.AutoWatch)
		p.Person = user.String()
		return p, err
	})
	if s.m.Preferences == nil {
		s.m.Preferences = []SitePreference{}
	}
	return err
}

// readSpaces lists the spaces, and counts their pages and files for the
// progress, which the snapshots of the spaces then write one by one.
func (s *siteSnapshot) readSpaces(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `SELECT id FROM space ORDER BY key`)
	if err != nil {
		return err
	}
	if s.spaces, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
		return err
	}
	return tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM page WHERE trashed_at IS NULL)
		     + (SELECT count(*) FROM attachment a JOIN page p ON p.id = a.page_id WHERE p.trashed_at IS NULL)`).
		Scan(&s.total)
}

// readNotCarried counts what the archive leaves out where a count tells the
// administrator how much to give again.
func (s *siteSnapshot) readNotCarried(ctx context.Context, tx db.DBTX) error {
	counts := map[string]string{
		"tokens":           `SELECT count(*) FROM api_token WHERE NOT for_render`,
		"armatureAccounts": `SELECT (SELECT count(*) FROM armature_token) + (SELECT count(*) FROM armature_grant)`,
		"exciterAccounts":  `SELECT count(*) FROM exciter_token`,
		"webhookSecrets":   `SELECT count(*) FROM webhook_endpoint`,
		"joinRequests":     `SELECT count(*) FROM join_request`,
		"links":            `SELECT (SELECT count(*) FROM page_share) + (SELECT count(*) FROM page_link)`,
	}
	s.m.NotCarried = make([]Omission, 0, len(notCarried))
	for _, o := range notCarried {
		if sql, ok := counts[o.Key]; ok {
			var n int
			if err := tx.QueryRow(ctx, sql).Scan(&n); err != nil {
				return fmt.Errorf("count %s: %w", o.What, err)
			}
			o.Count = &n
		}
		s.m.NotCarried = append(s.m.NotCarried, o)
	}
	return nil
}

// exportSite writes the organization's archive and stores it, answering its
// name and size; each space is written as its own archive inside it.
func (w *Watch) exportSite(ctx context.Context, c *siteClaim) (string, int64, error) {
	f, done, err := w.tempFile()
	if err != nil {
		return "", 0, err
	}
	defer done()
	zw := zip.NewWriter(f)
	p := newProgress(w, kindSiteExport, c.id, c.attempts)
	step := p.step(ctx)
	var snap *siteSnapshot
	err = w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if snap, err = readSite(ctx, tx, c.user); err != nil {
			return err
		}
		return writeAudit(ctx, tx, zw, snap.m)
	})
	if err != nil {
		return "", 0, err
	}
	p.setTotal(ctx, snap.total+len(snap.themeKey))
	for _, id := range snap.spaces {
		space, err := w.exportSiteSpace(ctx, zw, c, id, step)
		if err != nil {
			return "", 0, err
		}
		snap.m.Spaces = append(snap.m.Spaces, *space)
		snap.m.Counts.Spaces++
		snap.m.Counts.Pages += space.Counts.Pages
		snap.m.Counts.Versions += space.Counts.Versions
		snap.m.Counts.Files += space.Counts.Files
		snap.m.Counts.Comments += space.Counts.Comments
	}
	if err := snap.writeOwnFiles(ctx, w, zw, step); err != nil {
		return "", 0, err
	}
	snap.m.Counts.People, snap.m.Counts.Groups = len(snap.m.People), len(snap.m.Groups)
	if err := writeJSON(zw, manifestPath, snap.m); err != nil {
		return "", 0, err
	}
	if err := zw.Close(); err != nil {
		return "", 0, err
	}
	size, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return "", 0, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	if err := w.store.Put(ctx, c.object, f, size, "application/zip"); err != nil {
		return "", 0, err
	}
	p.flush(ctx, true)
	return siteDownloadName(snap.m.Organization.Slug, snap.m.ExportedAt), size, nil
}

// exportSiteSpace writes one space's archive to a file of its own and stores
// it in the organization's whole, so it reads as a space's archive alone.
func (w *Watch) exportSiteSpace(ctx context.Context, outer *zip.Writer, c *siteClaim, id uuid.UUID, step func()) (*SiteSpace, error) {
	f, done, err := w.tempFile()
	if err != nil {
		return nil, err
	}
	defer done()
	zw := zip.NewWriter(f)
	var (
		snap   *snapshot
		counts Counts
	)
	err = w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if snap, err = readWhole(ctx, tx, c.user, id); err != nil {
			return err
		}
		if err := snap.tooMany(); err != nil {
			return &siteSpaceTooLarge{key: snap.space.Key, err: err}
		}
		counts, err = snap.writeArchive(ctx, tx, zw, step)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := snap.writeFiles(ctx, w.store, zw, archiveFilePath, everyFile, step); err != nil {
		return nil, err
	}
	for _, p := range snap.pages {
		counts.Drafts += len(p.Drafts)
	}
	if err := writeJSON(zw, manifestPath, snap.manifest(counts)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	path := siteSpacePath(snap.space.Key)
	// Stored, not compressed: the import opens it in place, and its pages
	// are compressed inside it already.
	out, err := outer.CreateHeader(&zip.FileHeader{Name: path, Method: zip.Store, Modified: time.Now()})
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(out, f); err != nil {
		return nil, err
	}
	return &SiteSpace{Key: snap.space.Key, Name: snap.space.Name, Archive: path, Counts: counts}, nil
}

// siteSpaceTooLarge names the space an organization's export cannot hold.
type siteSpaceTooLarge struct {
	key string
	err error
}

func (e *siteSpaceTooLarge) Error() string {
	return fmt.Sprintf("The space %s holds more than one archive may (%d pages, %d files). Move some of its pages to another space, then export the organization again.", e.key, MaxPages, MaxFiles)
}

func (e *siteSpaceTooLarge) Unwrap() error { return e.err }

// writeOwnFiles copies the logo's and the themes' bytes into the archive.
func (s *siteSnapshot) writeOwnFiles(ctx context.Context, w *Watch, zw *zip.Writer, step func()) error {
	if s.m.Brand.Logo != nil {
		if err := copyObject(ctx, w.store, s.logoKey, zw, siteLogoPath, File{Name: "logo", Size: s.m.Brand.Logo.Size, CreatedAt: s.m.ExportedAt}); err != nil {
			return err
		}
	}
	for _, t := range s.m.Themes {
		for _, f := range t.Files {
			if err := copyObject(ctx, w.store, s.themeKey[f.ID], zw, siteThemeFilePath(f.ID), File{Name: f.Name, Size: f.Size, CreatedAt: s.m.ExportedAt}); err != nil {
				return err
			}
			step()
		}
	}
	return nil
}

// writeAudit writes the organization's audit log, oldest first, a line each.
func writeAudit(ctx context.Context, tx db.DBTX, zw *zip.Writer, m *SiteManifest) error {
	out, err := zw.CreateHeader(&zip.FileHeader{Name: siteAuditPath, Method: zip.Deflate})
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT actor_user_id, action, target_type, target_id, data, created_at FROM audit_log ORDER BY created_at, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for rows.Next() {
		var (
			e     AuditEntry
			actor *uuid.UUID
		)
		if err := rows.Scan(&actor, &e.Action, &e.TargetType, &e.TargetID, &e.Data, &e.At); err != nil {
			return err
		}
		e.Actor = idText(actor)
		if err := enc.Encode(e); err != nil {
			return err
		}
		m.AuditEntries++
	}
	return rows.Err()
}
