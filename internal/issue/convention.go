package issue

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"
)

// Type is what kind of work an issue is.
type Type int

const (
	TypeTask Type = iota
	TypeBug
	TypeFeature
	TypeDocs
)

var Types = []Type{TypeTask, TypeBug, TypeFeature, TypeDocs}

func (t Type) String() string {
	switch t {
	case TypeBug:
		return "Bug"
	case TypeFeature:
		return "Feature"
	case TypeDocs:
		return "Docs"
	default:
		return "Task"
	}
}

// Status is where an issue is in its life.
type Status int

const (
	StatusTodo Status = iota
	StatusIdea
	StatusInProgress
	StatusBlocked
	StatusDone
	StatusWontDo
)

// Statuses lists statuses in workflow order.
var Statuses = []Status{StatusIdea, StatusTodo, StatusInProgress, StatusBlocked, StatusDone, StatusWontDo}

func (s Status) String() string {
	switch s {
	case StatusIdea:
		return "Idea"
	case StatusInProgress:
		return "In progress"
	case StatusBlocked:
		return "Blocked"
	case StatusDone:
		return "Done"
	case StatusWontDo:
		return "Won't do"
	default:
		return "Todo"
	}
}

// ParseType accepts names like "bug", "Feature", "docs" or the convention's
// label names.
func (c Convention) ParseType(value string) (Type, error) {
	key := normalizeName(value)
	for _, t := range Types {
		if key == normalizeName(t.String()) || (t != TypeTask && key == normalizeName(c.TypeLabel(t))) {
			return t, nil
		}
	}
	switch key {
	case "feat", "enhancement":
		return TypeFeature, nil
	case "doc", "documentation":
		return TypeDocs, nil
	case "chore", "none":
		return TypeTask, nil
	}
	return TypeTask, fmt.Errorf("unknown type %q (use task, bug, feature, or docs)", value)
}

// ParseStatus accepts names like "idea", "in progress", "in-progress",
// "wontdo" or "done".
func (c Convention) ParseStatus(value string) (Status, error) {
	key := normalizeName(value)
	for _, s := range Statuses {
		if key == normalizeName(s.String()) {
			return s, nil
		}
	}
	switch key {
	case normalizeName(c.Idea):
		return StatusIdea, nil
	case normalizeName(c.InProgress), "active", "doing", "wip":
		return StatusInProgress, nil
	case normalizeName(c.Blocked):
		return StatusBlocked, nil
	case "open", "planned":
		return StatusTodo, nil
	case "closed", "completed":
		return StatusDone, nil
	case "notplanned", "dropped", "wontfix":
		return StatusWontDo, nil
	}
	return StatusTodo, fmt.Errorf("unknown status %q (use idea, todo, in progress, blocked, done, or wontdo)", value)
}

func normalizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer(" ", "", "-", "", "_", "", "'", "").Replace(value)
}

// Convention maps types and statuses to label names. The defaults reuse the
// labels GitHub creates in every new repository where possible.
type Convention struct {
	Bug        string `toml:"bug"`
	Feature    string `toml:"feature"`
	Docs       string `toml:"docs"`
	Idea       string `toml:"idea"`
	InProgress string `toml:"in_progress"`
	Blocked    string `toml:"blocked"`
}

func DefaultConvention() Convention {
	return Convention{
		Bug:        "bug",
		Feature:    "enhancement",
		Docs:       "documentation",
		Idea:       "idea",
		InProgress: "in progress",
		Blocked:    "blocked",
	}
}

// WithDefaults fills empty fields from DefaultConvention.
func (c Convention) WithDefaults() Convention {
	d := DefaultConvention()
	fill := func(value *string, fallback string) {
		if strings.TrimSpace(*value) == "" {
			*value = fallback
		}
		*value = strings.TrimSpace(*value)
	}
	fill(&c.Bug, d.Bug)
	fill(&c.Feature, d.Feature)
	fill(&c.Docs, d.Docs)
	fill(&c.Idea, d.Idea)
	fill(&c.InProgress, d.InProgress)
	fill(&c.Blocked, d.Blocked)
	return c
}

// TypeLabel returns the label for t, or "" for TypeTask.
func (c Convention) TypeLabel(t Type) string {
	switch t {
	case TypeBug:
		return c.Bug
	case TypeFeature:
		return c.Feature
	case TypeDocs:
		return c.Docs
	default:
		return ""
	}
}

// StatusLabel returns the label for s, or "" when the status is expressed by
// the issue's state alone.
func (c Convention) StatusLabel(s Status) string {
	switch s {
	case StatusIdea:
		return c.Idea
	case StatusInProgress:
		return c.InProgress
	case StatusBlocked:
		return c.Blocked
	default:
		return ""
	}
}

func (c Convention) typeLabels() []string { return []string{c.Bug, c.Feature, c.Docs} }

func (c Convention) statusLabels() []string { return []string{c.Idea, c.InProgress, c.Blocked} }

// IsConventionLabel reports whether label carries a type or status.
func (c Convention) IsConventionLabel(label string) bool {
	return containsFold(c.typeLabels(), label) || containsFold(c.statusLabels(), label) || strings.EqualFold(label, "feature")
}

// TypeOf derives an issue's type from its labels. "feature" counts as a
// feature even when the convention uses "enhancement".
func (c Convention) TypeOf(i Issue) Type {
	switch {
	case i.HasLabel(c.Bug):
		return TypeBug
	case i.HasLabel(c.Feature), i.HasLabel("feature"):
		return TypeFeature
	case i.HasLabel(c.Docs):
		return TypeDocs
	default:
		return TypeTask
	}
}

// StatusOf derives an issue's status from its state and labels.
func (c Convention) StatusOf(i Issue) Status {
	if !i.IsOpen() {
		if i.StateReason == ReasonNotPlanned {
			return StatusWontDo
		}
		return StatusDone
	}
	switch {
	case i.HasLabel(c.Blocked):
		return StatusBlocked
	case i.HasLabel(c.InProgress):
		return StatusInProgress
	case i.HasLabel(c.Idea):
		return StatusIdea
	default:
		return StatusTodo
	}
}

// Change is the set of edits that moves an issue to a new type or status.
type Change struct {
	AddLabels    []string
	RemoveLabels []string
	// State is "open" or "closed" when the state must change, else "".
	State       string
	StateReason string
}

func (ch Change) Empty() bool {
	return len(ch.AddLabels) == 0 && len(ch.RemoveLabels) == 0 && ch.State == ""
}

// SetType returns the label edits that give i type t.
func (c Convention) SetType(i Issue, t Type) Change {
	want := c.TypeLabel(t)
	var ch Change
	for _, label := range append(c.typeLabels(), "feature") {
		if label != "" && !strings.EqualFold(label, want) && i.HasLabel(label) {
			if t == TypeFeature && strings.EqualFold(label, "feature") {
				continue
			}
			ch.RemoveLabels = append(ch.RemoveLabels, label)
		}
	}
	if want != "" && c.TypeOf(i) != t {
		ch.AddLabels = append(ch.AddLabels, want)
	}
	return ch
}

// SetStatus returns the edits that give i status s: at most one status label
// on open issues, none on closed ones, and the matching close reason.
func (c Convention) SetStatus(i Issue, s Status) Change {
	var ch Change
	want := c.StatusLabel(s)
	for _, label := range c.statusLabels() {
		if !strings.EqualFold(label, want) && i.HasLabel(label) {
			ch.RemoveLabels = append(ch.RemoveLabels, label)
		}
	}
	if want != "" && !i.HasLabel(want) {
		ch.AddLabels = append(ch.AddLabels, want)
	}
	switch s {
	case StatusDone, StatusWontDo:
		reason := ReasonCompleted
		if s == StatusWontDo {
			reason = ReasonNotPlanned
		}
		if i.IsOpen() || i.StateReason != reason {
			ch.State, ch.StateReason = StateClosed, reason
		}
	default:
		if !i.IsOpen() {
			ch.State, ch.StateReason = StateOpen, "reopened"
		}
	}
	return ch
}

// Apply returns i with ch applied, as GitHub would after the change.
func (ch Change) Apply(i Issue) Issue {
	i.Labels = WithLabels(i.Labels, ch.AddLabels, ch.RemoveLabels)
	if ch.State != "" {
		i.State = ch.State
		i.StateReason = ch.StateReason
		if ch.State == StateOpen {
			i.StateReason = ""
			i.ClosedAt = time.Time{}
		} else if i.ClosedAt.IsZero() {
			i.ClosedAt = time.Now().UTC()
		}
	}
	return i
}

// LabelColor returns a stable hex color (without '#') for a label triage
// creates.
func (c Convention) LabelColor(label string) string {
	switch {
	case strings.EqualFold(label, c.Idea):
		return "c5def5"
	case strings.EqualFold(label, c.InProgress):
		return "fbca04"
	case strings.EqualFold(label, c.Blocked):
		return "b60205"
	case strings.EqualFold(label, c.Bug):
		return "d73a4a"
	case strings.EqualFold(label, c.Feature):
		return "a2eeef"
	case strings.EqualFold(label, c.Docs):
		return "0075ca"
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(strings.ToLower(label)))
	hash := hasher.Sum32()
	return hslToHex(float64(hash%360), 0.45, 0.5)
}

// LabelDescription is the description triage gives labels it creates.
func (c Convention) LabelDescription(label string) string {
	switch {
	case strings.EqualFold(label, c.Idea):
		return "Not committed to yet"
	case strings.EqualFold(label, c.InProgress):
		return "Someone is working on this"
	case strings.EqualFold(label, c.Blocked):
		return "Waiting on something else"
	}
	return ""
}

func hslToHex(h, s, l float64) string {
	h = math.Mod(h, 360) / 360
	var r, g, b float64
	if s == 0 {
		r, g, b = l, l, l
	} else {
		var q float64
		if l < 0.5 {
			q = l * (1 + s)
		} else {
			q = l + s - l*s
		}
		p := 2*l - q
		r = hueToRGB(p, q, h+1.0/3.0)
		g = hueToRGB(p, q, h)
		b = hueToRGB(p, q, h-1.0/3.0)
	}
	return fmt.Sprintf("%02x%02x%02x", int(math.Round(r*255)), int(math.Round(g*255)), int(math.Round(b*255)))
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	default:
		return p
	}
}
