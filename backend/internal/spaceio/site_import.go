package spaceio

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/brand"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/theme"
	"github.com/praetorianer777/stator/backend/internal/wikiread"
)

// auditBatch is how many audit entries one statement writes.
const auditBatch = 500

var (
	orgRoles       = []string{"owner", "admin", "member", "guest"}
	roleSources    = []string{"manual", "oidc"}
	groupSources   = []string{"local", "oidc"}
	globalGrants   = []string{"use", "createSpace"}
	providerRoles  = []string{"admin", "member"}
	logoTypes      = []string{"image/png", "image/jpeg", "image/webp"}
	personLocales  = []string{"en", "de"}
	digestSettings = []string{"off", "hourly", "daily"}
)

// readSiteManifest reads and checks an organization archive's table of contents.
func readSiteManifest(zr *zip.Reader) (*SiteManifest, error) {
	a := openArchive(zr)
	if a.entries[manifestPath] == nil {
		return nil, invalid("This file is not an organization archive of Stator: it holds no manifest.json. Export the organization from its settings in Stator, then import that file.")
	}
	data, err := a.read(manifestPath, MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	var m SiteManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, invalid("The archive's manifest.json is not one Stator reads. Export the organization again and import the new file.")
	}
	if err := CheckSiteManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// siteImporter fills one organization from one archive, in one transaction.
type siteImporter struct {
	w        *Watch
	m        *SiteManifest
	outer    *archive
	importer uuid.UUID
	spaces   []*importer
	// r holds every id the spaces' archives give, so a link or an include
	// from one space to another finds its page here.
	r renames
	// people and groups are the archive's refs here; guests wait for their space.
	people, groups map[string]uuid.UUID
	guests         []SitePerson
	themes         map[uuid.UUID]uuid.UUID
	spaceIDs       map[uuid.UUID]uuid.UUID
	seated         map[string]bool
	uploaded       []string
	report         SiteReport
	step           func()
}

// partError names the part of the organization a write failed on.
type partError struct {
	what string
	err  error
}

func (e *partError) Error() string { return fmt.Sprintf("%s: %v", e.what, e.err) }
func (e *partError) Unwrap() error { return e.err }

func part(what string, err error) error {
	if err == nil {
		return nil
	}
	return &partError{what: what, err: err}
}

// importSite fills the organization from the archive. Everything is read and
// checked once before the transaction, which writes it all and marks the
// job done, so a failure keeps nothing.
func (w *Watch) importSite(ctx context.Context, c *siteClaim) (db.LSN, error) {
	f, done, err := w.tempFile()
	if err != nil {
		return 0, err
	}
	defer done()
	body, err := w.store.Get(ctx, c.object)
	if err != nil {
		return 0, err
	}
	size, err := io.Copy(f, io.LimitReader(body, c.size+1))
	_ = body.Close()
	if err != nil {
		return 0, err
	}
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return 0, invalid("This file is no zip. Export the organization from its settings in Stator, then import that file.")
	}
	m, err := readSiteManifest(zr)
	if err != nil {
		return 0, err
	}
	oi := &siteImporter{w: w, m: m, outer: openArchive(zr), importer: c.user, r: newRenames(),
		people: map[string]uuid.UUID{}, groups: map[string]uuid.UUID{}, themes: map[uuid.UUID]uuid.UUID{},
		spaceIDs: map[uuid.UUID]uuid.UUID{}, seated: map[string]bool{}}
	oi.report.settle()
	oi.report.NotCarried = m.NotCarried
	if oi.report.NotCarried == nil {
		oi.report.NotCarried = []Omission{}
	}
	var closers []func()
	defer func() {
		for _, release := range closers {
			release()
		}
	}()
	total := 0
	seen := map[string]bool{}
	for _, sp := range m.Spaces {
		nested, release, err := w.openNested(oi.outer, sp.Archive, f)
		if err != nil {
			return 0, err
		}
		closers = append(closers, release)
		sm, err := readManifest(nested)
		if err != nil {
			return 0, err
		}
		im := &importer{src: archiveSource{openArchive(nested)}, m: sm, store: w.store, importer: c.user,
			spaceID: uuid.Must(uuid.NewV7()), from: SourceArchive, whole: true, r: oi.r,
			report: Report{People: []MissingPerson{}, Groups: []string{}, Dropped: []DroppedEntry{}, Lost: []wikiread.Loss{}}}
		im.key, im.name = sm.Space.Key, sm.Space.Name
		if err := checkSpace(im); err != nil {
			return 0, err
		}
		if seen[im.key] {
			return 0, invalid("The archive holds the space %s twice. Export the organization again and import the new file.", im.key)
		}
		seen[im.key] = true
		if err := im.scan(); err != nil {
			return 0, err
		}
		total += len(sm.Pages)
		oi.spaces = append(oi.spaces, im)
	}
	total += len(oi.r.files)
	for _, t := range m.Themes {
		total += len(t.Files)
	}
	p := newProgress(w, kindSiteImport, c.id, c.attempts)
	p.setTotal(ctx, total)
	oi.step = p.step(ctx)
	for _, im := range oi.spaces {
		im.step = oi.step
	}
	written, err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := oi.write(ctx, tx); err != nil {
			return err
		}
		report, err := json.Marshal(oi.report)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE site_import SET state = 'done', report = $3, lease_until = NULL, done_steps = total_steps, finished_at = now()
			WHERE id = $1 AND attempts = $2 AND state = 'running'`, c.id, c.attempts, report)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errLeaseLost
		}
		return nil
	})
	if err != nil {
		for _, im := range oi.spaces {
			oi.uploaded = append(oi.uploaded, im.uploaded...)
		}
		w.letGo(context.WithoutCancel(ctx), oi.uploaded)
		return 0, err
	}
	return written, nil
}

// openNested opens a space's archive inside the organization's: in place
// when it is stored as the export writes it, else unpacked to a file.
func (w *Watch) openNested(outer *archive, name string, file *os.File) (*zip.Reader, func(), error) {
	entry := outer.entries[name]
	if entry == nil {
		return nil, nil, invalid("The archive lacks %s, which its table of contents names. Export the organization again and import the new file.", name)
	}
	if entry.Method == zip.Store {
		off, err := entry.DataOffset()
		if err != nil {
			return nil, nil, invalid("The archive's %s could not be read. Export the organization again and import the new file.", name)
		}
		size := int64(entry.UncompressedSize64)
		zr, err := zip.NewReader(io.NewSectionReader(file, off, size), size)
		if err != nil {
			return nil, nil, invalid("The archive's %s is no space archive. Export the organization again and import the new file.", name)
		}
		return zr, func() {}, nil
	}
	tmp, done, err := w.tempFile()
	if err != nil {
		return nil, nil, err
	}
	rc, err := entry.Open()
	if err != nil {
		done()
		return nil, nil, invalid("The archive's %s could not be read. Export the organization again and import the new file.", name)
	}
	size, err := io.Copy(tmp, io.LimitReader(rc, MaxUnpackedBytes+1))
	_ = rc.Close()
	switch {
	case err != nil:
		done()
		return nil, nil, invalid("The archive's %s could not be read. Export the organization again and import the new file.", name)
	case size > MaxUnpackedBytes:
		done()
		return nil, nil, tooLarge("The archive's %s unpacks into more than %d GB, more than one import takes. Split the space before you export it.", name, MaxUnpackedBytes>>30)
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		done()
		return nil, nil, invalid("The archive's %s is no space archive. Export the organization again and import the new file.", name)
	}
	return zr, done, nil
}

// write makes everything of the archive in the caller's transaction: the
// people and groups first, as the spaces name them, then the themes, the
// spaces, the guests of each, and what hangs off all of them.
func (oi *siteImporter) write(ctx context.Context, tx db.DBTX) error {
	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM space)`).Scan(&taken); err != nil {
		return err
	}
	if taken {
		return ErrSiteNotEmpty
	}
	steps := []func() error{
		func() error { return oi.writePeople(ctx, tx) },
		func() error { return oi.writeGroups(ctx, tx) },
		func() error { return oi.writeThemes(ctx, tx) },
		func() error { return oi.writeSpaces(ctx, tx) },
		func() error { return oi.writeSettings(ctx, tx) },
		func() error { return oi.writeBrand(ctx, tx) },
		func() error { return oi.writePermissions(ctx, tx) },
		func() error { return oi.writeSignIn(ctx, tx) },
		func() error { return oi.writeArmature(ctx, tx) },
		func() error { return oi.writeTemplates(ctx, tx) },
		func() error { return oi.writeWebhooks(ctx, tx) },
		func() error { return oi.writePreferences(ctx, tx) },
		func() error { return oi.writePersonThemes(ctx, tx) },
		func() error { return oi.writeAuditLog(ctx, tx) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOrgImported, TargetType: "org", Actor: oi.importer, Data: map[string]any{
		"from": oi.m.Organization.Slug, "name": oi.m.Organization.Name, "spaces": oi.report.Spaces, "pages": oi.report.Pages,
		"people": oi.report.People, "groups": oi.report.Groups,
	}})
}

// person finds a person by ref, here or nowhere.
func (oi *siteImporter) person(ref string) *uuid.UUID {
	if id, ok := oi.people[ref]; ok {
		return &id
	}
	return nil
}

// writePeople finds everybody by their address, or makes their account, and
// lets them into the organization in the role they had; somebody who is a
// member here already keeps their role.
func (oi *siteImporter) writePeople(ctx context.Context, tx db.DBTX) error {
	for _, p := range oi.m.People {
		email := strings.TrimSpace(p.Email)
		if !strings.Contains(email, "@") {
			return invalid("The archive names a person without an address. Export the organization again and import the new file.")
		}
		if !slices.Contains(orgRoles, p.Role) || !slices.Contains(roleSources, p.RoleSource) {
			return invalid("The archive gives %s a role Stator does not know. Export the organization again and import the new file.", email)
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = email
		}
		var locale *string
		if p.Locale != nil && slices.Contains(personLocales, *p.Locale) {
			locale = p.Locale
		}
		timezone := p.Timezone
		if timezone == "" {
			timezone = "UTC"
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM app_user WHERE email = $1`, email).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.QueryRow(ctx, `
				INSERT INTO app_user (email, name, timezone, locale, is_active, show_in_readers) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
				email, name, timezone, locale, p.Active, p.ShowInReaders).Scan(&id); err != nil {
				return part("the person "+email, err)
			}
		case err != nil:
			return err
		default:
			oi.report.Matched++
		}
		oi.people[p.Ref] = id
		oi.report.People++
		if p.Role == "guest" {
			oi.guests = append(oi.guests, p)
			continue
		}
		source := p.RoleSource
		if p.Role == "owner" {
			source = "manual"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO member (user_id, role, role_source, created_at) VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id) DO NOTHING`, id, p.Role, source, p.JoinedAt); err != nil {
			return part("the membership of "+email, err)
		}
	}
	return nil
}

// writeGroups finds each group by its name, or makes it, with its members.
func (oi *siteImporter) writeGroups(ctx context.Context, tx db.DBTX) error {
	for _, g := range oi.m.Groups {
		name := strings.TrimSpace(g.Name)
		if name == "" || !slices.Contains(groupSources, g.Source) {
			return invalid("The archive holds a group without a name, or of a kind Stator does not know. Export the organization again and import the new file.")
		}
		ref := g.ExternalRef
		if g.Source != "oidc" {
			ref = nil
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM groups WHERE lower(name) = lower($1)`, name).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.QueryRow(ctx, `
				INSERT INTO groups (name, description, source, external_ref) VALUES ($1, $2, $3, $4) RETURNING id`,
				name, g.Description, g.Source, ref).Scan(&id); err != nil {
				return part("the group "+name, err)
			}
		case err != nil:
			return err
		}
		oi.groups[g.Ref] = id
		oi.report.Groups++
		for _, member := range g.Members {
			user := oi.person(member)
			if user == nil || oi.isGuest(member) {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO group_member (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, *user); err != nil {
				return part("the members of the group "+name, err)
			}
		}
	}
	return nil
}

func (oi *siteImporter) isGuest(ref string) bool {
	return slices.ContainsFunc(oi.guests, func(p SitePerson) bool { return p.Ref == ref })
}

// writeThemes makes each theme its owner's again, its files under new ids
// that its spec is rewritten to name.
func (oi *siteImporter) writeThemes(ctx context.Context, tx db.DBTX) error {
	for _, t := range oi.m.Themes {
		owner := oi.importer
		if id := oi.person(t.Owner); id != nil {
			owner = *id
		}
		var spec theme.Spec
		if err := json.Unmarshal(t.Spec, &spec); err != nil {
			return invalid("The archive's theme %q is not one Stator reads. Export the organization again and import the new file.", t.Name)
		}
		name := strings.TrimSpace(t.Name)
		for n := 2; ; n++ {
			var taken bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM theme WHERE owner_id = $1 AND lower(name) = lower($2) AND armature_theme_id IS NULL)`,
				owner, name).Scan(&taken); err != nil {
				return err
			}
			if !taken {
				break
			}
			name = fmt.Sprintf("%s (%d)", strings.TrimSpace(t.Name), n)
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO theme (id, owner_id, name, shared) VALUES ($1, $2, $3, $4)`, id, owner, name, t.Shared); err != nil {
			return part("the theme "+name, err)
		}
		renamed := map[uuid.UUID]uuid.UUID{}
		files := map[uuid.UUID]bool{}
		for _, file := range t.Files {
			data, err := oi.outer.read(siteThemeFilePath(file.ID), theme.MaxAssetBytes)
			if err != nil {
				return err
			}
			fileName, contentType, err := theme.CheckFile(file.Name, data)
			if err != nil {
				return invalid("The archive's theme %q holds a file Stator does not accept: %s Export the organization again and import the new file.", t.Name, sentenceOf(err))
			}
			asset := uuid.Must(uuid.NewV7())
			var key string
			if err := tx.QueryRow(ctx, `
				INSERT INTO theme_asset (id, theme_id, name, content_type, size) VALUES ($1, $2, $3, $4, $5) RETURNING object_key`,
				asset, id, fileName, contentType, len(data)).Scan(&key); err != nil {
				return part("the theme "+name, err)
			}
			if err := oi.w.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
				return err
			}
			oi.uploaded = append(oi.uploaded, key)
			renamed[file.ID], files[asset] = asset, true
			oi.step()
		}
		if err := theme.RenameFiles(&spec, id, renamed); err != nil {
			return invalid("The archive's theme %q names a file it does not hold. Export the organization again and import the new file.", t.Name)
		}
		if err := theme.Validate(&spec, id, files); err != nil {
			return invalid("The archive's theme %q is not one Stator accepts: %s Export the organization again and import the new file.", t.Name, sentenceOf(err))
		}
		encoded, err := json.Marshal(spec)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE theme SET spec = $2 WHERE id = $1`, id, encoded); err != nil {
			return part("the theme "+name, err)
		}
		oi.themes[t.ID] = id
		oi.report.Themes++
	}
	return nil
}

// writeSpaces makes each space as its own archive would, with the
// organization's people and groups found by ref before any address.
func (oi *siteImporter) writeSpaces(ctx context.Context, tx db.DBTX) error {
	for _, im := range oi.spaces {
		im.presetPeople, im.presetGroups = oi.people, oi.groups
		im.made = func(ctx context.Context, tx db.DBTX) error { return oi.writeGuests(ctx, tx, im.key, im.spaceID) }
		if err := im.mapPeople(ctx, tx); err != nil {
			return err
		}
		if err := im.write(ctx, tx); err != nil {
			return err
		}
		if im.m.Space.ID != uuid.Nil {
			oi.spaceIDs[im.m.Space.ID] = im.spaceID
		}
		r := &im.report
		oi.report.Spaces++
		oi.report.Pages += r.Pages
		oi.report.Versions += r.Versions
		oi.report.Files += r.Files
		oi.report.Comments += r.Comments
		oi.report.Drafts += r.Drafts
		oi.report.Templates += r.Templates
		oi.report.Reattributed += r.Reattributed
		oi.report.DroppedReactions += r.DroppedReactions
		for _, d := range r.Dropped {
			if len(oi.report.Dropped) < maxReported {
				oi.report.Dropped = append(oi.report.Dropped, d)
			}
		}
	}
	for _, g := range oi.guests {
		if !oi.seated[g.Ref] {
			oi.report.Dropped = append(oi.report.Dropped, DroppedEntry{Permission: "guest", Subject: fmt.Sprintf("%s <%s>", g.Name, g.Email)})
		}
	}
	return nil
}

// writeGuests lets the guests of a space in as soon as it exists, before
// its grants name them; a guest whose space is not in the archive is not.
func (oi *siteImporter) writeGuests(ctx context.Context, tx db.DBTX, key string, space uuid.UUID) error {
	for _, g := range oi.guests {
		if g.GuestSpace != key {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO member (user_id, role, role_source, guest_space_id, created_at) VALUES ($1, 'guest', 'manual', $2, $3)
			ON CONFLICT (user_id) DO NOTHING`, oi.people[g.Ref], space, g.JoinedAt); err != nil {
			return part("the guest "+g.Email, err)
		}
		oi.seated[g.Ref] = true
	}
	return nil
}

// writeSettings gives the organization the archive's name and switches, its
// hub and its default theme; it keeps its own address.
func (oi *siteImporter) writeSettings(ctx context.Context, tx db.DBTX) error {
	set := oi.m.Settings
	var hub, defaultTheme *uuid.UUID
	if set.HubPage != nil {
		if id, ok := oi.r.pages[*set.HubPage]; ok {
			hub = &id
		}
	}
	if set.DefaultTheme != nil {
		if id, ok := oi.themes[*set.DefaultTheme]; ok {
			defaultTheme = &id
		}
	}
	name := strings.TrimSpace(oi.m.Organization.Name)
	_, err := tx.Exec(ctx, `
		UPDATE site SET name = COALESCE(NULLIF($1, ''), name), anonymous_access = $2, anonymous_indexable = $3, public_links = $4,
		       hub_page_id = $5, hub_landing = $6, default_theme_id = $7`,
		name, set.AnonymousAccess, set.AnonymousIndexable, set.PublicLinks, hub, set.HubLanding && hub != nil, defaultTheme)
	return part("the organization's settings", err)
}

// writeBrand gives the exports their footer lines and logo back.
func (oi *siteImporter) writeBrand(ctx context.Context, tx db.DBTX) error {
	b := oi.m.Brand
	_, err := tx.Exec(ctx, `
		INSERT INTO brand (footer_en, footer_de, updated_by) VALUES ($1, $2, $3)
		ON CONFLICT (singleton) DO UPDATE SET footer_en = EXCLUDED.footer_en, footer_de = EXCLUDED.footer_de, updated_by = EXCLUDED.updated_by`,
		clip(b.FooterEn, brand.MaxFooterLength), clip(b.FooterDe, brand.MaxFooterLength), oi.importer)
	if err != nil {
		return part("the brand", err)
	}
	if b.Logo == nil {
		return nil
	}
	data, err := oi.outer.read(siteLogoPath, brand.MaxLogoBytes)
	if err != nil {
		return err
	}
	kind := http.DetectContentType(data)
	if !slices.Contains(logoTypes, kind) {
		return invalid("The archive's logo is not a PNG, JPEG or WebP picture. Export the organization again and import the new file.")
	}
	var key string
	if err := tx.QueryRow(ctx, `
		UPDATE brand SET logo_type = $1, logo_size = $2, logo_version = logo_version + 1 RETURNING logo_key`,
		kind, len(data)).Scan(&key); err != nil {
		return part("the logo", err)
	}
	if err := oi.w.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), kind); err != nil {
		return err
	}
	oi.uploaded = append(oi.uploaded, key)
	return nil
}

// subject is a grant's subject here, or false when nobody here answers to it.
func (oi *siteImporter) subject(s Subject) (user, group *uuid.UUID, ok bool) {
	switch s.Type {
	case "user":
		user = oi.person(s.Person)
		return user, nil, user != nil
	case "group":
		if id, found := oi.groups[s.Group]; found {
			return nil, &id, true
		}
		return nil, nil, false
	case "everyone":
		return nil, nil, true
	}
	return nil, nil, false
}

// writePermissions replaces what a new organization grants by the archive's.
func (oi *siteImporter) writePermissions(ctx context.Context, tx db.DBTX) error {
	if _, err := tx.Exec(ctx, `DELETE FROM global_grant`); err != nil {
		return err
	}
	for _, g := range oi.m.Permissions {
		user, group, ok := oi.subject(g.Subject)
		if !ok || !slices.Contains(globalGrants, g.Permission) {
			oi.report.Dropped = append(oi.report.Dropped, DroppedEntry{Permission: g.Permission, Subject: g.Subject.Type})
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO global_grant (permission, subject_type, user_id, group_id) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
			g.Permission, g.Subject.Type, user, group); err != nil {
			return part("the organization's permissions", err)
		}
	}
	return nil
}

// writeSignIn brings the provider across switched off, without its secret,
// unless the organization has one of its own, which stays.
func (oi *siteImporter) writeSignIn(ctx context.Context, tx db.DBTX) error {
	s := oi.m.SignIn
	if s == nil {
		return nil
	}
	if s.Issuer != "" {
		tag, err := tx.Exec(ctx, `
			INSERT INTO oidc_provider (issuer, client_id, groups_claim, scopes, create_groups, enabled)
			VALUES ($1, $2, $3, $4, $5, false) ON CONFLICT (singleton) DO NOTHING`,
			s.Issuer, s.ClientID, s.GroupsClaim, s.Scopes, s.CreateGroups)
		if err != nil {
			return part("the sign-in provider", err)
		}
		if tag.RowsAffected() == 0 {
			oi.report.Kept = append(oi.report.Kept, "signIn")
		}
	}
	for _, r := range s.GroupRoles {
		if !slices.Contains(providerRoles, r.Role) || strings.TrimSpace(r.Group) == "" {
			continue
		}
		// A role belongs to the provider now; an older archive may carry
		// roles without one, which would have nothing here to hang off.
		if _, err := tx.Exec(ctx, `
			INSERT INTO oidc_group_role (group_ref, role) SELECT $1, $2 WHERE EXISTS (SELECT 1 FROM oidc_provider)
			ON CONFLICT (group_ref) DO NOTHING`,
			r.Group, r.Role); err != nil {
			return part("the roles the provider's groups give", err)
		}
	}
	return nil
}

// writeArmature brings the Armature connection across without its webhook
// secret, unless the organization has one of its own.
func (oi *siteImporter) writeArmature(ctx context.Context, tx db.DBTX) error {
	a := oi.m.Armature
	if a == nil {
		return nil
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO armature_connection (base_url, org_slug, updated_by) VALUES ($1, $2, $3) ON CONFLICT (singleton) DO NOTHING`,
		a.BaseURL, a.OrgSlug, oi.importer)
	if err != nil {
		return part("the Armature connection", err)
	}
	if tag.RowsAffected() == 0 {
		oi.report.Kept = append(oi.report.Kept, "armature")
	}
	return nil
}

// writeTemplates makes the templates every space offers, pointed at the
// pages they name here.
func (oi *siteImporter) writeTemplates(ctx context.Context, tx db.DBTX) error {
	r := oi.r
	for _, t := range oi.m.Templates {
		body, err := r.rewrite(t.Body)
		if err != nil {
			return invalid("The archive's template %q is not one Stator reads. Export the organization again and import the new file.", t.Name)
		}
		if _, err := document.ParseTemplate(body); err != nil {
			return invalid("The archive's template %q is not one Stator accepts: %s Export the organization again and import the new file.", t.Name, sentenceOf(err))
		}
		variables := t.Variables
		if len(variables) == 0 {
			variables = json.RawMessage("[]")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_template (space_id, name, description, title, body, variables)
			VALUES (NULL, $1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, t.Name, t.Description, t.Title, body, variables); err != nil {
			return part(fmt.Sprintf("the template %q", t.Name), err)
		}
		oi.report.Templates++
	}
	return nil
}

// writeWebhooks brings each webhook across switched off, with no secret that
// signs: its secret is rotated before it is switched on.
func (oi *siteImporter) writeWebhooks(ctx context.Context, tx db.DBTX) error {
	for _, h := range oi.m.Webhooks {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO webhook_endpoint (name, url, secret_sealed, topics, enabled, version) VALUES ($1, $2, ''::bytea, $3, false, $4)
			RETURNING id`, h.Name, h.URL, h.Topics, max(h.Version, 1)).Scan(&id); err != nil {
			return part(fmt.Sprintf("the webhook %q", h.Name), err)
		}
		// Inserted, its owner is whoever acts; a change of owner afterwards
		// is kept as written.
		if owner := oi.person(h.Owner); owner != nil {
			if _, err := tx.Exec(ctx, `UPDATE webhook_endpoint SET owner_id = $2 WHERE id = $1`, id, *owner); err != nil {
				return part(fmt.Sprintf("the webhook %q", h.Name), err)
			}
		}
		oi.report.Webhooks++
	}
	return nil
}

func (oi *siteImporter) writePreferences(ctx context.Context, tx db.DBTX) error {
	for _, p := range oi.m.Preferences {
		user := oi.person(p.Person)
		if user == nil || !slices.Contains(digestSettings, p.Digest) {
			continue
		}
		inApp, email := p.InApp, p.Email
		if len(inApp) == 0 {
			inApp = json.RawMessage("{}")
		}
		if len(email) == 0 {
			email = json.RawMessage("{}")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO notification_preference (user_id, in_app, email, digest, auto_watch) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (user_id) DO NOTHING`, *user, inApp, email, p.Digest, p.AutoWatch); err != nil {
			return part("the notification settings", err)
		}
	}
	return nil
}

func (oi *siteImporter) writePersonThemes(ctx context.Context, tx db.DBTX) error {
	for _, p := range oi.m.PersonThemes {
		user := oi.person(p.Person)
		if user == nil {
			continue
		}
		var chosen *uuid.UUID
		if p.Theme != nil {
			id, ok := oi.themes[*p.Theme]
			if !ok {
				continue
			}
			chosen = &id
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_theme (user_id, theme_id, follow_armature) VALUES ($1, $2, $3) ON CONFLICT (user_id) DO NOTHING`,
			*user, chosen, p.FollowArmature && chosen == nil); err != nil {
			return part("the themes people chose", err)
		}
	}
	return nil
}

// writeAuditLog writes the organization's record as it was, its people and
// what it names found here where the import made them anew.
func (oi *siteImporter) writeAuditLog(ctx context.Context, tx db.DBTX) error {
	if oi.outer.entries[siteAuditPath] == nil {
		return nil
	}
	ids := map[uuid.UUID]uuid.UUID{}
	for _, m := range []map[uuid.UUID]uuid.UUID{oi.r.pages, oi.r.files, oi.r.threads, oi.r.calendars, oi.spaceIDs, oi.themes} {
		for old, fresh := range m {
			ids[old] = fresh
		}
	}
	for _, refs := range []map[string]uuid.UUID{oi.people, oi.groups} {
		for ref, fresh := range refs {
			if old, err := uuid.Parse(ref); err == nil {
				ids[old] = fresh
			}
		}
	}
	rc, err := oi.outer.entries[siteAuditPath].Open()
	if err != nil {
		return invalid("The archive's audit.jsonl could not be read. Export the organization again and import the new file.")
	}
	defer rc.Close()
	dec := json.NewDecoder(io.LimitReader(rc, MaxUnpackedBytes))
	var batch auditRows
	for {
		var e AuditEntry
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return invalid("The archive's audit.jsonl is not one Stator reads. Export the organization again and import the new file.")
		}
		if strings.TrimSpace(e.Action) == "" {
			continue
		}
		batch.add(e, oi.person(e.Actor), ids)
		if len(batch.action) >= auditBatch {
			if err := batch.flush(ctx, tx); err != nil {
				return err
			}
			oi.report.AuditEntries += auditBatch
		}
	}
	oi.report.AuditEntries += len(batch.action)
	return batch.flush(ctx, tx)
}

// auditRows are audit entries gathered for one statement, as text a
// statement casts, so an absent actor or target is an empty string.
type auditRows struct {
	actor, action, targetType, target, data, at []string
}

func (b *auditRows) add(e AuditEntry, actor *uuid.UUID, ids map[uuid.UUID]uuid.UUID) {
	who, target := "", ""
	if actor != nil {
		who = actor.String()
	}
	if e.TargetID != nil {
		t := *e.TargetID
		if fresh, ok := ids[t]; ok {
			t = fresh
		}
		target = t.String()
	}
	data := string(e.Data)
	if data == "" || data == "null" {
		data = "{}"
	}
	b.actor, b.action, b.targetType = append(b.actor, who), append(b.action, e.Action), append(b.targetType, e.TargetType)
	b.target, b.data, b.at = append(b.target, target), append(b.data, data), append(b.at, e.At.UTC().Format("2006-01-02T15:04:05.999999Z07:00"))
}

func (b *auditRows) flush(ctx context.Context, tx db.DBTX) error {
	if len(b.action) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor_user_id, action, target_type, target_id, data, created_at)
		SELECT NULLIF(a, '')::uuid, act, tt, NULLIF(tid, '')::uuid, d::jsonb, at::timestamptz
		FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[]) AS e(a, act, tt, tid, d, at)`,
		b.actor, b.action, b.targetType, b.target, b.data, b.at)
	*b = auditRows{}
	return part("the audit log", err)
}

// siteFailureOf reads why an organization's import failed beyond what a
// space's import says, naming the part a refused row belonged to.
func siteFailureOf(err error) (Failure, string) {
	var (
		pgErr *pgconn.PgError
		at    *partError
	)
	switch {
	case errors.Is(err, ErrSiteNotEmpty):
		return FailureNotEmpty, ""
	case errors.As(err, &pgErr) && strings.HasPrefix(pgErr.Code, "23") && errors.As(err, &at):
		return FailureInvalid, fmt.Sprintf("The archive's %s could not be saved as it is (%s). Export the organization again and import the new file.", at.what, pgErr.ConstraintName)
	}
	failure, detail := failureOf(err)
	if failure == FailureKeyTaken {
		return FailureNotEmpty, ""
	}
	return failure, detail
}
