// Package titles normalises window titles for reporting, so that titles
// differing only in a notification count ("(3) YouTube", "Inbox (12) -
// Gmail") add up as one. Recorded titles are never changed.
package titles

import (
	"fmt"
	"regexp"
	"strings"
)

// Rule replaces matches of Pattern (a Go regular expression, see
// https://pkg.go.dev/regexp/syntax) with Replace, which may refer to
// groups as $1, ${name}, ...
type Rule struct {
	Pattern string `json:"pattern"`
	Replace string `json:"replace"`
}

// DefaultRules strip the usual notification counts and unread markers.
var DefaultRules = []Rule{
	// A count at the start: "(3) YouTube", "(12+) Feed | LinkedIn",
	// "[2] Discord".
	{Pattern: `^\s*[(\[]\d+\+?[)\]]\s*`},
	// An unread marker at the start: "* general | Slack", "• Chat".
	{Pattern: `^\s*[*•●]\s+`},
	// A count after a folder or section name, anywhere:
	// "Inbox (12) - you@gmail.com - Gmail", "Chat (2) | Teams".
	{Pattern: `(?i)\b(inbox|chats?|messages|notifications|unread|feed|mentions)\s*\(\d+\+?\)`, Replace: "$1"},
}

// Normalizer applies rules in order.
type Normalizer struct {
	rules []compiled
}

type compiled struct {
	re      *regexp.Regexp
	replace string
}

// New compiles rules; the error names the first invalid one.
func New(rules []Rule) (*Normalizer, error) {
	n := &Normalizer{}
	for i, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("title rule %d (%s): %w", i+1, r.Pattern, err)
		}
		n.rules = append(n.rules, compiled{re: re, replace: r.Replace})
	}
	return n, nil
}

// Default returns a Normalizer with DefaultRules.
func Default() *Normalizer {
	n, err := New(DefaultRules)
	if err != nil {
		panic(err) // the defaults are constant and tested
	}
	return n
}

// Normalize returns title with the rules applied and surrounding space
// trimmed. A title the rules would empty is returned unchanged.
func (n *Normalizer) Normalize(title string) string {
	if n == nil || len(n.rules) == 0 {
		return title
	}
	out := title
	for _, r := range n.rules {
		out = r.re.ReplaceAllString(out, r.replace)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return title
	}
	return out
}

// Parse reads rules written one per line as "pattern => replacement" (the
// replacement, and the arrow, may be left out to delete matches). Blank
// lines and lines starting with "#" are skipped.
func Parse(text string) ([]Rule, error) {
	var rules []Rule
	for i, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		var r Rule
		if p, rep, ok := strings.Cut(line, " => "); ok {
			r = Rule{Pattern: strings.TrimSpace(p), Replace: strings.TrimSpace(rep)}
		} else {
			r = Rule{Pattern: t}
		}
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// Format writes rules in the form Parse reads.
func Format(rules []Rule) string {
	var b strings.Builder
	for _, r := range rules {
		b.WriteString(r.Pattern)
		if r.Replace != "" {
			b.WriteString(" => " + r.Replace)
		}
		b.WriteString("\n")
	}
	return b.String()
}
