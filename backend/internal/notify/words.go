package notify

import (
	"fmt"
	"strings"

	"github.com/praetorianer777/stator/backend/internal/mail"
)

// PreferencesPath is where the web client lets a person choose what they hear.
const PreferencesPath = "/settings/notifications"

// someone names an actor who is no longer in the organization.
const someone = "Somebody"

// mailed is one notification as a mail words it. Mail is the one place the
// server writes sentences: the client words the rows itself.
type mailed struct {
	kind     Kind
	actor    string
	title    string
	spaceKey string
	subject  Subject
}

// Sentence says what happened, in English; inComment is a mention made in
// a comment rather than in the page.
func Sentence(kind Kind, actor, title string, inComment bool) string {
	if actor == "" {
		actor = someone
	}
	quoted := `"` + title + `"`
	switch kind {
	case KindAssigned:
		return fmt.Sprintf("%s assigned you a task on %s", actor, quoted)
	case KindDue:
		return fmt.Sprintf("A task of yours on %s is due", quoted)
	case KindMentioned:
		if inComment {
			return fmt.Sprintf("%s mentioned you in a comment on %s", actor, quoted)
		}
		return fmt.Sprintf("%s mentioned you on %s", actor, quoted)
	case KindShared:
		return fmt.Sprintf("%s shared %s with you", actor, quoted)
	case KindReplied:
		return fmt.Sprintf("%s replied in a thread on %s", actor, quoted)
	case KindCommented:
		return fmt.Sprintf("%s commented on %s", actor, quoted)
	case KindResolved:
		return fmt.Sprintf("%s resolved or reopened a thread on %s", actor, quoted)
	case KindPublished:
		return fmt.Sprintf("%s published a new version of %s", actor, quoted)
	case KindCreated:
		return fmt.Sprintf("%s created %s", actor, quoted)
	case KindExpired:
		return fmt.Sprintf("The verification of %s has run out; check the page and verify it again", quoted)
	case KindFailed:
		return fmt.Sprintf("Your scheduled publish of %s did not go out; open the page to see why and schedule it again", quoted)
	}
	return fmt.Sprintf("%s changed %s", actor, quoted)
}

// PageURL is where a notification leads: the page, with its thread open when
// there is one. The client puts the slug right.
func PageURL(appURL, spaceKey string, s Subject) string {
	link := fmt.Sprintf("%s/s/%s/p/%s", appURL, spaceKey, s.PageID)
	if s.ThreadID != nil {
		link += "?thread=" + s.ThreadID.String()
	}
	return link
}

func (m mailed) sentence() string {
	return Sentence(m.kind, m.actor, m.title, m.subject.CommentID != nil)
}

// single is the mail for one notification sent at once.
func (m mailed) single(to, appURL string) mail.Mail {
	var b strings.Builder
	b.WriteString(m.sentence() + ".\n\n")
	if m.subject.Excerpt != "" {
		b.WriteString(m.subject.Excerpt + "\n\n")
	}
	fmt.Fprintf(&b, "Open the page: %s\n\n", PageURL(appURL, m.spaceKey, m.subject))
	fmt.Fprintf(&b, "Choose what you are told about: %s%s\n", appURL, PreferencesPath)
	return mail.Mail{To: to, Subject: m.sentence(), Body: b.String()}
}

// bundle is the mail for everything a digest gathered.
func bundle(to, appURL string, items []mailed) mail.Mail {
	var b strings.Builder
	noun := "updates"
	if len(items) == 1 {
		noun = "update"
	}
	fmt.Fprintf(&b, "%d %s since your last mail:\n\n", len(items), noun)
	for _, m := range items {
		fmt.Fprintf(&b, "- %s\n  %s\n", m.sentence(), PageURL(appURL, m.spaceKey, m.subject))
		if m.subject.Excerpt != "" {
			fmt.Fprintf(&b, "  %s\n", m.subject.Excerpt)
		}
	}
	fmt.Fprintf(&b, "\nChoose what you are told about: %s%s\n", appURL, PreferencesPath)
	return mail.Mail{To: to, Subject: fmt.Sprintf("%d %s in Stator", len(items), noun), Body: b.String()}
}
