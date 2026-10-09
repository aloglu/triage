package issue

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Query is a parsed filter such as
//
//	is:open repo:triage label:bug assignee:@me status:"in progress" crash
//
// Qualifiers can be negated with a leading "-". Bare words match the title,
// body, or issue number.
type Query struct {
	terms []term
	raw   string
}

type term struct {
	key    string
	value  string
	negate bool
}

// MatchContext supplies what a query needs besides the issue itself.
type MatchContext struct {
	Me         string
	Convention Convention
}

var queryKeys = map[string]bool{
	"is": true, "repo": true, "label": true, "assignee": true, "author": true,
	"type": true, "status": true, "no": true,
}

// QueryKeys lists the qualifiers ParseQuery understands, for help text.
func QueryKeys() []string {
	return []string{"is:open|closed", "repo:", "label:", "assignee:@me", "author:", "type:", "status:", "no:assignee|label"}
}

// ParseQuery parses a filter. Unknown qualifiers are treated as text so that
// typing "foo:bar" still finds issues containing it.
func ParseQuery(raw string) Query {
	q := Query{raw: strings.TrimSpace(raw)}
	for _, token := range tokenize(raw) {
		t := term{value: token}
		body := token
		if strings.HasPrefix(body, "-") && len(body) > 1 {
			body = body[1:]
			t.negate = true
		}
		if key, value, ok := strings.Cut(body, ":"); ok && queryKeys[strings.ToLower(key)] {
			t.key = strings.ToLower(key)
			t.value = unquote(value)
		} else {
			t.negate = false
			t.value = unquote(token)
		}
		if t.value == "" {
			continue
		}
		q.terms = append(q.terms, t)
	}
	return q
}

func (q Query) String() string { return q.raw }

func (q Query) Empty() bool { return len(q.terms) == 0 }

// Has reports whether the query contains a qualifier with key, such as "is".
func (q Query) Has(key string) bool {
	for _, t := range q.terms {
		if t.key == key {
			return true
		}
	}
	return false
}

// Match reports whether i satisfies every term.
func (q Query) Match(i Issue, ctx MatchContext) bool {
	for _, t := range q.terms {
		if t.match(i, ctx) == t.negate {
			return false
		}
	}
	return true
}

// Validate returns an error describing the first qualifier value that can
// never match, such as "type:nope".
func (q Query) Validate(conv Convention) error {
	for _, t := range q.terms {
		switch t.key {
		case "is":
			if v := strings.ToLower(t.value); v != "open" && v != "closed" {
				return fmt.Errorf("is: takes open or closed")
			}
		case "type":
			if _, err := conv.ParseType(t.value); err != nil {
				return err
			}
		case "status":
			if _, err := conv.ParseStatus(t.value); err != nil {
				return err
			}
		case "no":
			if v := strings.ToLower(t.value); v != "assignee" && v != "label" {
				return fmt.Errorf("no: takes assignee or label")
			}
		}
	}
	return nil
}

func (t term) match(i Issue, ctx MatchContext) bool {
	value := t.value
	switch t.key {
	case "is":
		switch strings.ToLower(value) {
		case "open":
			return i.IsOpen()
		case "closed":
			return !i.IsOpen()
		}
		return false
	case "repo":
		return strings.EqualFold(i.Repo, value) || strings.EqualFold(repoName(i.Repo), value)
	case "label":
		return i.HasLabel(value)
	case "assignee":
		return i.IsAssigned(resolveMe(value, ctx.Me))
	case "author":
		return strings.EqualFold(i.Author, resolveMe(value, ctx.Me))
	case "type":
		want, err := ctx.Convention.ParseType(value)
		return err == nil && ctx.Convention.TypeOf(i) == want
	case "status":
		want, err := ctx.Convention.ParseStatus(value)
		return err == nil && ctx.Convention.StatusOf(i) == want
	case "no":
		switch strings.ToLower(value) {
		case "assignee":
			return len(i.Assignees) == 0
		case "label":
			return len(i.Labels) == 0
		}
		return false
	default:
		return matchText(i, value)
	}
}

func matchText(i Issue, value string) bool {
	if n, err := strconv.Atoi(strings.TrimPrefix(value, "#")); err == nil && n == i.Number {
		return true
	}
	needle := strings.ToLower(value)
	return strings.Contains(strings.ToLower(i.Title), needle) || strings.Contains(strings.ToLower(i.Body), needle)
}

func resolveMe(value, me string) string {
	if strings.EqualFold(value, "@me") {
		return me
	}
	return strings.TrimPrefix(value, "@")
}

func repoName(repo string) string {
	if _, name, ok := strings.Cut(repo, "/"); ok {
		return name
	}
	return repo
}

// tokenize splits on whitespace, keeping double-quoted sections together:
// status:"in progress" is one token.
func tokenize(raw string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	for _, r := range raw {
		switch {
		case r == '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case unicode.IsSpace(r) && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func unquote(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, `"`, ""))
}

// Without returns q minus every term using qualifier key.
func (q Query) Without(key string) Query {
	out := Query{raw: q.raw}
	for _, t := range q.terms {
		if t.key != key {
			out.terms = append(out.terms, t)
		}
	}
	return out
}

// And returns a query matching both q and other.
func (q Query) And(other Query) Query {
	out := Query{raw: strings.TrimSpace(q.raw + " " + other.raw)}
	out.terms = append(append(out.terms, q.terms...), other.terms...)
	return out
}
