// Package patterns matches text (window titles) against lists of
// patterns, one per line, in the syntax of trackz-viz's filter:
//
//   - a plain line is a phrase matched on whole words, ignoring case:
//     "Jim Smith" matches "Re: Jim Smith's PR" but not "Jim Smithson", and
//     "trackz" matches "trackz-viz" (only letters and digits make up words;
//     any space in the phrase matches any run of spaces);
//   - a line written /.../ is a regular expression (Go syntax), ignoring
//     case;
//   - blank lines are ignored.
package patterns

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Set is a compiled list of patterns. The zero Set has none.
type Set struct {
	lines []string
	res   []*regexp.Regexp
}

// Parse compiles the patterns in text, one per line.
func Parse(text string) (Set, error) { return Compile(Lines(text)) }

// Lines splits text into its patterns: trimmed, non-blank lines.
func Lines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Compile compiles patterns (see the package documentation); blank ones
// are skipped. The error names the first invalid one.
func Compile(patterns []string) (Set, error) {
	var s Set
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		re, err := compile(p)
		if err != nil {
			return Set{}, err
		}
		s.lines = append(s.lines, p)
		s.res = append(s.res, re)
	}
	return s, nil
}

// isRegexp reports whether p is written as a regular expression (/.../).
func isRegexp(p string) bool {
	return len(p) >= 2 && strings.HasPrefix(p, "/") && strings.HasSuffix(p, "/")
}

func compile(p string) (*regexp.Regexp, error) {
	if isRegexp(p) {
		re, err := regexp.Compile("(?i)" + p[1:len(p)-1])
		if err != nil {
			return nil, fmt.Errorf("pattern %s: %w", p, unwrapSyntax(err))
		}
		return re, nil
	}
	return regexp.MustCompile("(?i)" + phrase(p)), nil
}

// unwrapSyntax drops regexp's "error parsing regexp: " prefix.
func unwrapSyntax(err error) error {
	var se *syntax.Error
	if errors.As(err, &se) {
		return errors.New(se.Code.String())
	}
	return err
}

// phrase is the expression for a plain pattern: its words in order,
// separated by spaces, with a word boundary at either end that is a
// letter or a digit.
func phrase(p string) string {
	expr := strings.Join(quoteWords(strings.Fields(p)), `\s+`)
	first, _ := utf8.DecodeRuneInString(p)
	last, _ := utf8.DecodeLastRuneInString(p)
	if isWord(first) {
		expr = `(?:^|[^\pL\pN])` + expr
	}
	if isWord(last) {
		expr += `(?:[^\pL\pN]|$)`
	}
	return expr
}

func quoteWords(ws []string) []string {
	for i, w := range ws {
		ws[i] = regexp.QuoteMeta(w)
	}
	return ws
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// Empty reports whether s has no patterns.
func (s Set) Empty() bool { return len(s.res) == 0 }

// Patterns returns the patterns, as given (trimmed).
func (s Set) Patterns() []string { return s.lines }

// Match reports whether any pattern matches text.
func (s Set) Match(text string) bool {
	for _, re := range s.res {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}
