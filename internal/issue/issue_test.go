package issue

import (
	"reflect"
	"testing"
)

func TestTypeAndStatusFromLabels(t *testing.T) {
	conv := DefaultConvention()
	tests := []struct {
		name   string
		issue  Issue
		typ    Type
		status Status
	}{
		{"plain open issue", Issue{State: StateOpen}, TypeTask, StatusTodo},
		{"bug in progress", Issue{State: StateOpen, Labels: []string{"bug", "in progress"}}, TypeBug, StatusInProgress},
		{"feature alias", Issue{State: StateOpen, Labels: []string{"Feature", "idea"}}, TypeFeature, StatusIdea},
		{"enhancement", Issue{State: StateOpen, Labels: []string{"enhancement"}}, TypeFeature, StatusTodo},
		{"blocked wins over in progress", Issue{State: StateOpen, Labels: []string{"in progress", "blocked"}}, TypeTask, StatusBlocked},
		{"closed completed ignores labels", Issue{State: StateClosed, StateReason: ReasonCompleted, Labels: []string{"blocked"}}, TypeTask, StatusDone},
		{"closed not planned", Issue{State: StateClosed, StateReason: ReasonNotPlanned, Labels: []string{"documentation"}}, TypeDocs, StatusWontDo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := conv.TypeOf(tt.issue); got != tt.typ {
				t.Errorf("TypeOf = %v, want %v", got, tt.typ)
			}
			if got := conv.StatusOf(tt.issue); got != tt.status {
				t.Errorf("StatusOf = %v, want %v", got, tt.status)
			}
		})
	}
}

func TestSetStatusKeepsOneStatusLabel(t *testing.T) {
	conv := DefaultConvention()
	open := Issue{State: StateOpen, Labels: []string{"bug", "idea"}}

	ch := conv.SetStatus(open, StatusInProgress)
	if !reflect.DeepEqual(ch.AddLabels, []string{"in progress"}) || !reflect.DeepEqual(ch.RemoveLabels, []string{"idea"}) || ch.State != "" {
		t.Fatalf("SetStatus(in progress) = %+v", ch)
	}
	if got := ch.Apply(open); conv.StatusOf(got) != StatusInProgress || !reflect.DeepEqual(got.Labels, []string{"bug", "in progress"}) {
		t.Fatalf("applied issue = %+v", got)
	}

	ch = conv.SetStatus(open, StatusWontDo)
	if ch.State != StateClosed || ch.StateReason != ReasonNotPlanned || !reflect.DeepEqual(ch.RemoveLabels, []string{"idea"}) {
		t.Fatalf("SetStatus(won't do) = %+v", ch)
	}

	closed := Issue{State: StateClosed, StateReason: ReasonCompleted}
	ch = conv.SetStatus(closed, StatusTodo)
	if ch.State != StateOpen || len(ch.AddLabels) != 0 {
		t.Fatalf("reopen = %+v", ch)
	}
	if got := conv.SetStatus(closed, StatusDone); !got.Empty() {
		t.Fatalf("no-op status change = %+v", got)
	}
}

func TestSetType(t *testing.T) {
	conv := DefaultConvention()
	i := Issue{State: StateOpen, Labels: []string{"bug", "ui"}}
	ch := conv.SetType(i, TypeFeature)
	if !reflect.DeepEqual(ch.AddLabels, []string{"enhancement"}) || !reflect.DeepEqual(ch.RemoveLabels, []string{"bug"}) {
		t.Fatalf("SetType(feature) = %+v", ch)
	}
	ch = conv.SetType(i, TypeTask)
	if len(ch.AddLabels) != 0 || !reflect.DeepEqual(ch.RemoveLabels, []string{"bug"}) {
		t.Fatalf("SetType(task) = %+v", ch)
	}
	if ch := conv.SetType(Issue{Labels: []string{"feature"}}, TypeFeature); !ch.Empty() {
		t.Fatalf("feature alias should already count as a feature: %+v", ch)
	}
}

func TestCustomConvention(t *testing.T) {
	conv := Convention{InProgress: "wip", Feature: "feature"}.WithDefaults()
	i := Issue{State: StateOpen, Labels: []string{"wip", "feature"}}
	if conv.StatusOf(i) != StatusInProgress || conv.TypeOf(i) != TypeFeature {
		t.Fatalf("custom labels not recognised: %v %v", conv.StatusOf(i), conv.TypeOf(i))
	}
	if s, err := conv.ParseStatus("wip"); err != nil || s != StatusInProgress {
		t.Fatalf("ParseStatus(wip) = %v, %v", s, err)
	}
}

func TestParseNames(t *testing.T) {
	conv := DefaultConvention()
	for input, want := range map[string]Status{"in-progress": StatusInProgress, "In Progress": StatusInProgress, "wontdo": StatusWontDo, "won't do": StatusWontDo, "todo": StatusTodo} {
		if got, err := conv.ParseStatus(input); err != nil || got != want {
			t.Errorf("ParseStatus(%q) = %v, %v", input, got, err)
		}
	}
	for input, want := range map[string]Type{"bug": TypeBug, "enhancement": TypeFeature, "docs": TypeDocs, "task": TypeTask} {
		if got, err := conv.ParseType(input); err != nil || got != want {
			t.Errorf("ParseType(%q) = %v, %v", input, got, err)
		}
	}
	if _, err := conv.ParseStatus("nope"); err == nil {
		t.Error("ParseStatus(nope) should fail")
	}
}

func TestQuery(t *testing.T) {
	ctx := MatchContext{Me: "octo", Convention: DefaultConvention()}
	issue := Issue{
		Repo: "aloglu/bookshelf", Number: 12, Title: "Overflowing top bar", Body: "on mobile",
		State: StateOpen, Labels: []string{"bug", "in progress"}, Assignees: []string{"octo"}, Author: "someone",
	}
	tests := []struct {
		query string
		want  bool
	}{
		{"", true},
		{"is:open", true},
		{"is:closed", false},
		{"repo:bookshelf", true},
		{"repo:aloglu/bookshelf", true},
		{"repo:triage", false},
		{"-repo:triage", true},
		{"label:bug", true},
		{"-label:bug", false},
		{"assignee:@me", true},
		{"author:someone", true},
		{"type:bug status:\"in progress\"", true},
		{"status:in-progress", true},
		{"status:blocked", false},
		{"no:assignee", false},
		{"top bar", true},
		{"MOBILE", true},
		{"#12", true},
		{"12", true},
		{"desktop", false},
		{"weird:thing", false},
	}
	for _, tt := range tests {
		if got := ParseQuery(tt.query).Match(issue, ctx); got != tt.want {
			t.Errorf("%q matched = %v, want %v", tt.query, got, tt.want)
		}
	}
	if err := ParseQuery("type:nope").Validate(ctx.Convention); err == nil {
		t.Error("Validate should reject unknown type")
	}
}

func TestWithLabels(t *testing.T) {
	got := WithLabels([]string{"Bug", "ui"}, []string{"bug", "idea"}, []string{"UI"})
	if !reflect.DeepEqual(got, []string{"Bug", "idea"}) {
		t.Fatalf("WithLabels = %v", got)
	}
}
