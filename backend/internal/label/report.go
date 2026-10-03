package label

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// MaxReportPages caps one properties report; past it, Truncated says so.
const MaxReportPages = 200

var (
	// ErrNoReportLabel refuses a report that names no label to gather pages by.
	ErrNoReportLabel = errors.New("name at least one label; the report lists the pages that carry all of them")
	// ErrReportLabels refuses more labels than a report takes.
	ErrReportLabels = fmt.Errorf("name at most %d labels", document.MaxReportLabels)
	// ErrReportColumns refuses more columns, or a name no property could have.
	ErrReportColumns = fmt.Errorf("name at most %d properties, each up to %d characters, or none for all of them", document.MaxReportColumns, document.MaxPropertyKeyLength)
)

// ReportInput is what a properties report gathers: the pages carrying every
// label, in one space or all, and the properties to show, all when none.
type ReportInput struct {
	Labels   []string
	SpaceKey string
	Columns  []string
}

// ReportRow is one page and its value for each column, null where it has none.
type ReportRow struct {
	PageID    uuid.UUID            `json:"pageId"`
	Title     string               `json:"title"`
	SpaceKey  string               `json:"spaceKey"`
	UpdatedAt time.Time            `json:"updatedAt"`
	Values    []*document.Property `json:"values"`
}

// PropertiesReport is a register built from the pages' own properties.
// Columns are the names asked for, or every name found, first seen first.
type PropertiesReport struct {
	Columns   []string    `json:"columns"`
	Rows      []ReportRow `json:"rows"`
	Truncated bool        `json:"truncated"`
}

// reportPage is one page the report found, with its properties.
type reportPage struct {
	row   ReportRow
	props []document.Property
}

// checkReport normalizes the labels and tidies the columns.
func checkReport(in ReportInput) (labels, columns []string, err error) {
	if len(in.Labels) == 0 {
		return nil, nil, fieldError("label", ErrNoReportLabel)
	}
	if len(in.Labels) > document.MaxReportLabels {
		return nil, nil, fieldError("label", ErrReportLabels)
	}
	for _, raw := range in.Labels {
		name, err := Normalize(raw)
		if err != nil {
			return nil, nil, fieldError("label", err)
		}
		if !slices.Contains(labels, name) {
			labels = append(labels, name)
		}
	}
	if len(in.Columns) > document.MaxReportColumns {
		return nil, nil, fieldError("column", ErrReportColumns)
	}
	for _, raw := range in.Columns {
		key := strings.Join(strings.Fields(raw), " ")
		if key == "" || utf8.RuneCountInString(key) > document.MaxPropertyKeyLength {
			return nil, nil, fieldError("column", ErrReportColumns)
		}
		columns = append(columns, key)
	}
	return labels, columns, nil
}

// PropertiesReport gathers the properties of the published pages out of the
// trash and the archive that carry every label and that the actor may read.
func (s *Service) PropertiesReport(ctx context.Context, actor perm.Actor, in ReportInput) (*PropertiesReport, error) {
	labels, columns, err := checkReport(in)
	if err != nil {
		return nil, err
	}
	var pages []reportPage
	truncated := false
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		args := []any{actor.UserID, labels, MaxReportPages + 1}
		within := ""
		if key := strings.TrimSpace(in.SpaceKey); key != "" {
			sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
			if err != nil {
				return err
			}
			args = append(args, sp.ID)
			within = ` AND p.space_id = $4`
		}
		// The body is the published one: a draft is its author's until it is.
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, s.key, p.updated_at, p.body
			FROM page p JOIN space s ON s.id = p.space_id
			WHERE p.trashed_at IS NULL AND p.archived_at IS NULL AND p.version > 0
			  AND (SELECT count(*) FROM page_label l WHERE l.org_id = p.org_id AND l.page_id = p.id AND l.name = ANY($2)) = cardinality($2::text[])
			  AND `+perm.ViewablePage("p", 1)+within+`
			ORDER BY lower(p.title), p.id
			LIMIT $3`, args...)
		if err != nil {
			return fmt.Errorf("read the pages of the report: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				f    reportPage
				body []byte
			)
			if err := rows.Scan(&f.row.PageID, &f.row.Title, &f.row.SpaceKey, &f.row.UpdatedAt, &body); err != nil {
				return err
			}
			if len(pages) == MaxReportPages {
				truncated = true
				break
			}
			root, err := document.Parse(body)
			if err != nil {
				return fmt.Errorf("read the body of page %s: %w", f.row.PageID, err)
			}
			f.props = document.Properties(root)
			pages = append(pages, f)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return tabulate(pages, columns, truncated), nil
}

// tabulate lines each page's properties up under the columns asked for, or
// under every name found, first seen first, up to MaxReportColumns.
func tabulate(pages []reportPage, columns []string, truncated bool) *PropertiesReport {
	out := &PropertiesReport{Columns: columns, Rows: []ReportRow{}, Truncated: truncated}
	if len(columns) == 0 {
		out.Columns = []string{}
		seen := map[string]bool{}
		for _, p := range pages {
			for _, prop := range p.props {
				if name := document.PropertyName(prop.Key); !seen[name] && len(out.Columns) < document.MaxReportColumns {
					seen[name] = true
					out.Columns = append(out.Columns, prop.Key)
				}
			}
		}
	}
	for _, p := range pages {
		row, props := p.row, p.props
		row.Values = make([]*document.Property, len(out.Columns))
		for i, col := range out.Columns {
			for j := range props {
				if document.PropertyName(props[j].Key) == document.PropertyName(col) {
					row.Values[i] = &props[j]
					break
				}
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}
