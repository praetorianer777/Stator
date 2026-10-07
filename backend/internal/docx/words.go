package docx

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// words are what a document says beyond the page's own, in the reader's
// language: labels, the line under the title, and sentences for generated blocks.
type words struct {
	open, decided, undecided, expand, contents, diagram, somebody string
	includedElsewhere, includeMissing, attachmentList, childPages string
	panels                                                        map[string]string
	charts                                                        map[string]string
	months                                                        [12]string
	german                                                        bool
}

var english = words{
	open: "Open in Stator", decided: "Decided", undecided: "Undecided", expand: "Details", contents: "Contents",
	diagram:           "A diagram, drawn in Stator from this source:",
	somebody:          "somebody",
	includedElsewhere: "Another page is shown here in Stator. Open it there to read it.",
	includeMissing:    "Another page is shown here to those who may read it.",
	attachmentList:    "A list of the files on this page." + seeInStator,
	childPages:        "A list of the pages below this one." + seeInStator,
	panels:            map[string]string{"info": "Info", "note": "Note", "success": "Success", "warning": "Warning", "error": "Error"},
	charts:            map[string]string{"bar": "A bar chart", "line": "A line chart", "pie": "A pie chart"},
	months:            [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
}

var german = words{
	open: "In Stator öffnen", decided: "Entschieden", undecided: "Offen", expand: "Details", contents: "Inhalt",
	diagram:           "Ein Diagramm, das Stator aus diesem Quelltext zeichnet:",
	somebody:          "jemand",
	includedElsewhere: "Hier zeigt Stator eine andere Seite. Öffnen Sie sie dort, um sie zu lesen.",
	includeMissing:    "Hier steht eine andere Seite für alle, die sie lesen dürfen.",
	attachmentList:    "Eine Liste der Dateien dieser Seite." + seeInStatorDE,
	childPages:        "Eine Liste der Seiten unter dieser." + seeInStatorDE,
	panels:            map[string]string{"info": "Info", "note": "Notiz", "success": "Erfolg", "warning": "Warnung", "error": "Fehler"},
	charts:            map[string]string{"bar": "Ein Balkendiagramm", "line": "Ein Liniendiagramm", "pie": "Ein Kreisdiagramm"},
	months:            [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
	german:            true,
}

// What each reader sees of a generated block is read when they open the page.
const (
	seeInStator   = " Open the page in Stator to see it as it is now."
	seeInStatorDE = " Öffnen Sie die Seite in Stator, um den aktuellen Stand zu sehen."
)

func wordsFor(lang string) words {
	if strings.EqualFold(lang, "de") {
		return german
	}
	return english
}

func (w words) see() string {
	if w.german {
		return seeInStatorDE
	}
	return seeInStator
}

func (w words) pick(en, de string) string {
	if w.german {
		return de
	}
	return en
}

// in names the space a block is held to, or nothing for every space.
func (w words) in(space string) string {
	if space == "" {
		return ""
	}
	return w.pick(" in ", " in ") + space
}

func (w words) version(n int, day string) string {
	if day == "" {
		return "Version " + strconv.Itoa(n)
	}
	return w.pick(fmt.Sprintf("Version %d of %s", n, day), fmt.Sprintf("Version %d vom %s", n, day))
}

// day writes a day in words, as the reader's language writes it.
func (w words) day(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.UTC()
	month := w.months[t.Month()-1]
	if w.german {
		return strconv.Itoa(t.Day()) + ". " + month + " " + strconv.Itoa(t.Year())
	}
	return month + " " + strconv.Itoa(t.Day()) + ", " + strconv.Itoa(t.Year())
}

// date writes a date node's day, or the day as stored when it is none.
func (w words) date(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return day
	}
	return w.day(t)
}

func (w words) tableChart(kind string) string {
	chart, ok := w.charts[kind]
	if !ok {
		chart = w.charts["bar"]
	}
	return chart + w.pick(" of this table is drawn in Stator.", " dieser Tabelle steht in Stator.")
}

func (w words) pictureMissing(alt string) string {
	if strings.TrimSpace(alt) == "" {
		return w.pick("A picture here could not be included.", "Ein Bild an dieser Stelle konnte nicht übernommen werden.")
	}
	return w.pick("A picture could not be included: ", "Ein Bild konnte nicht übernommen werden: ") + alt
}

func (w words) picturesMissing(n int) string {
	if n == 1 {
		return w.pick("One picture of this gallery could not be included.", "Ein Bild dieser Galerie konnte nicht übernommen werden.")
	}
	return fmt.Sprintf(w.pick("%d pictures of this gallery could not be included.", "%d Bilder dieser Galerie konnten nicht übernommen werden."), n)
}

func (w words) includedFrom(title string) string {
	return w.pick("Included from ", "Eingebunden aus ") + title
}

func (w words) labels(labels []string, all bool) string {
	join := w.pick(" or ", " oder ")
	if all {
		join = w.pick(" and ", " und ")
	}
	return strings.Join(labels, join)
}

func (w words) propertiesReport(labels []string, space string) string {
	return w.pick("A report of the properties of the pages labelled ", "Ein Bericht der Eigenschaften der Seiten mit dem Label ") +
		strings.Join(labels, ", ") + w.in(space) + "." + w.see()
}

func (w words) labelledPages(labels []string, all bool, space string) string {
	return w.pick("A list of the pages labelled ", "Eine Liste der Seiten mit dem Label ") + w.labels(labels, all) + w.in(space) + "." + w.see()
}

func (w words) recentlyUpdated(space string) string {
	return w.pick("A list of the pages updated last", "Eine Liste der zuletzt aktualisierten Seiten") + w.in(space) + "." + w.see()
}

func (w words) blogPosts(space string) string {
	if space == "" {
		return w.pick("A list of the latest blog posts of every space.", "Eine Liste der neuesten Blogbeiträge aller Bereiche.") + w.see()
	}
	return w.pick("A list of the latest blog posts", "Eine Liste der neuesten Blogbeiträge") + w.in(space) + "." + w.see()
}

func (w words) taskReport(space string) string {
	return w.pick("A report of tasks", "Ein Bericht über Aufgaben") + w.in(space) + "." + w.see()
}

func (w words) calendar(project string) string {
	if project == "" {
		return w.pick("A month of a calendar.", "Ein Monat eines Kalenders.") + w.see()
	}
	return w.pick("A month of a calendar, with the issues due in ", "Ein Monat eines Kalenders mit den fälligen Vorgängen aus ") + project + "." + w.see()
}

func (w words) templateButton(label string) string {
	out := w.pick("A button that makes a page from a template", "Eine Schaltfläche, die eine Seite aus einer Vorlage erstellt")
	if strings.TrimSpace(label) != "" {
		out += ": " + label
	}
	return out + "." + w.pick(" Use it on the page in Stator.", " Sie wirkt auf der Seite in Stator.")
}

func (w words) contributors(tree bool) string {
	if tree {
		return w.pick("The people who published this page and the pages below it.", "Die Personen, die diese Seite und die Seiten darunter veröffentlicht haben.") + w.see()
	}
	return w.pick("The people who published this page.", "Die Personen, die diese Seite veröffentlicht haben.") + w.see()
}

func (w words) issue(key string) string {
	return w.pick("Armature issue ", "Armature-Vorgang ") + key + "." + w.see()
}

func (w words) issueList(query string) string {
	return w.pick("A list of Armature issues: ", "Eine Liste von Armature-Vorgängen: ") + query + "." + w.see()
}

func (w words) issueChart(project, query string) string {
	return w.pick("An Armature chart of ", "Ein Armature-Diagramm von ") + project + ": " + query + "." + w.see()
}

func (w words) roadmap(project, query string) string {
	return w.pick("An Armature roadmap of ", "Eine Armature-Roadmap von ") + project + ": " + query + "." + w.see()
}
