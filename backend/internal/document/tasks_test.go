package document

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const (
	taskOne = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b01"
	taskTwo = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4b02"
)

func taskItem(id string, checked bool, inline string, nested ...string) string {
	attrs := `{"checked":` + map[bool]string{true: "true", false: "false"}[checked]
	if id != "" {
		attrs += `,"taskId":"` + id + `"`
	}
	attrs += "}"
	content := `{"type":"paragraph","content":[` + inline + `]}`
	if len(nested) > 0 {
		content += `,{"type":"taskList","content":[` + strings.Join(nested, ",") + `]}`
	}
	return `{"type":"taskItem","attrs":` + attrs + `,"content":[` + content + `]}`
}

func taskDoc(items ...string) string {
	return `{"type":"doc","content":[{"type":"taskList","content":[` + strings.Join(items, ",") + `]}]}`
}

func text(s string) string { return `{"type":"text","text":"` + s + `"}` }

func parsed(t *testing.T, body string) Node {
	t.Helper()
	if err := Validate(json.RawMessage(body)); err != nil {
		t.Fatalf("the test document is refused: %v", err)
	}
	root, err := Parse(json.RawMessage(body))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestATaskIsItsWordsItsFirstMentionAndItsFirstDate(t *testing.T) {
	body := taskDoc(
		taskItem(taskOne, false, text("Send the minutes ")+","+mentionNode(ada, "Ada")+","+text(" by ")+","+date("2026-10-09")+","+mentionNode(alan, "Alan")+","+date("2026-10-30"),
			taskItem(taskTwo, true, text("Book a room ")+","+mentionNode(grace, "Grace"))),
		taskItem("", false, text("Nobody's, someday")),
	)
	ada, grace := uuid.MustParse(ada), uuid.MustParse(grace)
	got := Tasks(parsed(t, body))
	want := []Task{
		{ID: uuid.MustParse(taskOne), Text: "Send the minutes @Ada by 2026-10-09@Alan2026-10-30", Assignee: &ada, Due: "2026-10-09"},
		{ID: uuid.MustParse(taskTwo), Done: true, Text: "Book a room @Grace", Assignee: &grace},
		{Text: "Nobody's, someday"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the tasks are\n%+v\nwant\n%+v", got, want)
	}
}

func TestATaskInsideOtherBlocksIsStillATask(t *testing.T) {
	body := `{"type":"doc","content":[{"type":"panel","attrs":{"kind":"info"},"content":[` +
		`{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[` +
		`{"type":"taskList","content":[` + taskItem(taskOne, false, text("Deep")) + `]}]}]}]}]}]}`
	got := Tasks(parsed(t, body))
	if len(got) != 1 || got[0].Text != "Deep" || got[0].ID != uuid.MustParse(taskOne) {
		t.Fatalf("the tasks are %+v", got)
	}
	if got := TasksIn(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`)); len(got) != 0 {
		t.Errorf("a page without a checklist has tasks %+v", got)
	}
	if got := TasksIn(nil); got != nil {
		t.Errorf("no body has tasks %+v", got)
	}
}

func TestATaskSaysAtMostItsLongestWords(t *testing.T) {
	long := strings.Repeat("word ", MaxTaskTextLength)
	got := Tasks(parsed(t, taskDoc(taskItem("", false, text(long)))))
	if n := len([]rune(got[0].Text)); n > MaxTaskTextLength || n < MaxTaskTextLength-5 {
		t.Fatalf("a long task keeps %d characters", n)
	}
}

func TestATaskIdMustBeAUUID(t *testing.T) {
	for _, bad := range []string{`"t1"`, `"0199A1B2-C3D4-7E5F-8A9B-0C1D2E3F4B01"`, `12`} {
		body := `{"type":"doc","content":[{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":false,"taskId":` + bad + `},"content":[{"type":"paragraph"}]}]}]}`
		if err := Validate(json.RawMessage(body)); err == nil {
			t.Errorf("a task id %s is accepted", bad)
		}
	}
	nullID := `{"type":"doc","content":[{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":false,"taskId":null},"content":[{"type":"paragraph"}]}]}]}`
	if err := Validate(json.RawMessage(nullID)); err != nil {
		t.Errorf("an item the editor has not given an id yet is refused: %v", err)
	}
}

func TestSettlingGivesEveryItemAnIdOfItsOwn(t *testing.T) {
	body := taskDoc(
		taskItem(taskOne, false, text("Kept")),
		taskItem(taskOne, false, text("A pasted copy"), taskItem("", false, text("Nested and new"))),
		taskItem("", false, text("Written in Markdown")),
	)
	root := parsed(t, body)
	previous := []Task{{ID: uuid.MustParse(taskTwo), Text: "Written in Markdown"}, {ID: uuid.MustParse(taskOne), Text: "A pasted copy"}}
	if !SettleTasks(&root, previous) {
		t.Fatal("settling changed nothing")
	}
	got := Tasks(root)
	if got[0].ID != uuid.MustParse(taskOne) {
		t.Errorf("the first item lost its id: %v", got[0].ID)
	}
	if got[3].ID != uuid.MustParse(taskTwo) {
		t.Errorf("an item without an id did not take the earlier task with its words: %v", got[3].ID)
	}
	seen := map[uuid.UUID]bool{}
	for _, task := range got {
		if task.ID == uuid.Nil || seen[task.ID] {
			t.Errorf("%q has the id %v, given twice or not at all", task.Text, task.ID)
		}
		seen[task.ID] = true
	}
	if err := ValidateNode(root); err != nil {
		t.Errorf("the settled document is refused: %v", err)
	}
	if SettleTasks(&root, previous) {
		t.Error("settling a settled document changed it")
	}
}

func TestSettlingAStoredBodyLeavesOneWithIdsAlone(t *testing.T) {
	body := json.RawMessage(taskDoc(taskItem(taskOne, false, text("Kept"))))
	got, err := SettleTasksIn(body, nil)
	if err != nil || string(got) != string(body) {
		t.Fatalf("a settled body came back as %s, %v", got, err)
	}
	fresh, err := SettleTasksIn(json.RawMessage(taskDoc(taskItem("", false, text("New")))), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tasks := TasksIn(fresh); len(tasks) != 1 || tasks[0].ID == uuid.Nil {
		t.Fatalf("the new item has %+v", tasks)
	}
	if _, err := SettleTasksIn(json.RawMessage(`{"type":`), nil); err == nil {
		t.Error("a body that does not parse is settled")
	}
}

func TestPlainTextReadsATasksAssigneeAndDay(t *testing.T) {
	root := parsed(t, taskDoc(taskItem(taskOne, false, text("Ship it ")+","+mentionNode(ada, "Ada")+","+text(" ")+","+date("2026-10-09"))))
	if got := PlainText(root); got != "Ship it @Ada 2026-10-09" {
		t.Fatalf("the plain text is %q", got)
	}
}
