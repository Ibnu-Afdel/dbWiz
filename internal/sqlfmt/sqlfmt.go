package sqlfmt

import "strings"

// indent is the width of one nesting level. Two spaces, matching the rest of
// DBWiz's TUI conventions (no tabs anywhere in rendered output).
const indentWidth = 2

// listBreakWidth is how long a SELECT list or INSERT column list has to be
// (rendered on one line) before it's worth breaking one item per line. Below
// it, the clause break alone (SELECT/FROM/WHERE each on their own line) is
// already enough structure — a three-column SELECT doesn't need one line per
// column any more than a short WHERE needs one condition per line.
const listBreakWidth = 60

// lineKeywords start a new line at the current nesting depth. Composites
// ("GROUP BY", "LEFT JOIN") are looked up by their merged text (see
// mergeComposites in lex.go) so the phrase breaks as one unit, not on its
// first word.
var lineKeywords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "GROUP BY": true,
	"ORDER BY": true, "HAVING": true, "LIMIT": true, "OFFSET": true,
	"INSERT INTO": true, "VALUES": true, "UPDATE": true, "SET": true,
	"DELETE FROM": true, "CREATE": true, "ALTER": true, "DROP": true,
	"WITH": true, "UNION": true, "UNION ALL": true, "RETURNING": true,
	"JOIN": true, "INNER JOIN": true, "LEFT JOIN": true, "RIGHT JOIN": true,
	"FULL JOIN": true, "CROSS JOIN": true, "LEFT OUTER JOIN": true,
	"RIGHT OUTER JOIN": true, "FULL OUTER JOIN": true,
}

// Format reformats sql into clause-per-line, keyword-cased SQL. It's a
// token-based rewrite (see lex.go), not a parser: it recognizes clause
// keywords and bracket/string/comment boundaries well enough to break and
// indent the statement shapes DBWiz's editor actually sees, and otherwise
// reproduces the input unchanged. Format is idempotent
// (Format(Format(s)) == Format(s)) and never edits the content of a string,
// quoted identifier, or comment — only the whitespace and keyword casing
// around them.
//
// Multiple ';'-separated statements are each formatted independently and
// joined by a blank line. Input sqlfmt can't safely reason about — currently
// just Postgres dollar-quoting ($$...$$ / $tag$...$tag$, whose body is
// arbitrary, often non-SQL, text — function bodies, for instance) — is
// returned unchanged rather than risked.
func Format(sql string) string {
	if strings.TrimSpace(sql) == "" {
		return sql
	}
	if hasDollarQuote(sql) {
		return sql
	}
	toks := lex(sql)
	if len(toks) == 0 {
		return sql
	}
	groups := splitStatements(toks)
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		parts = append(parts, formatStatement(g))
	}
	return strings.Join(parts, "\n\n")
}

// hasDollarQuote reports whether sql contains a Postgres dollar-quote opening
// tag: '$', then zero or more identifier characters, then another '$'. That's
// enough to catch both the untagged $$...$$ and tagged $func$...$func$ forms
// without a false positive on a numbered placeholder like $1 (a digit
// followed by end-of-token, never a second '$').
func hasDollarQuote(sql string) bool {
	r := []rune(sql)
	for i := range r {
		if r[i] != '$' {
			continue
		}
		j := i + 1
		for j < len(r) && isIdentPart(r[j]) {
			j++
		}
		if j < len(r) && r[j] == '$' {
			return true
		}
	}
	return false
}

// splitStatements groups tokens into one slice per top-level (depth 0)
// ';'-terminated statement, the trailing ';' included in the group it ends. A
// final statement with no trailing ';' is still its own group.
func splitStatements(tokens []token) [][]token {
	var groups [][]token
	var cur []token
	depth := 0
	for _, t := range tokens {
		switch {
		case t.kind == tPunct && t.text == "(":
			depth++
		case t.kind == tPunct && t.text == ")":
			depth--
		}
		cur = append(cur, t)
		if depth <= 0 && t.kind == tPunct && t.text == ";" {
			groups = append(groups, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

// formatStatement prints one statement's tokens with clause line breaks,
// list breaks (computeBreaks), and depth-based indentation.
func formatStatement(tokens []token) string {
	if len(tokens) == 0 {
		return ""
	}
	forceBreak, closeParenOwnLine := computeBreaks(tokens)

	var b strings.Builder
	depth := 0
	atLineStart := true
	var prev *token
	// lastKeyword is the most recently emitted keyword's text, used only to
	// tell a table-name's "(" (INSERT INTO t (…), CREATE/ALTER TABLE t (…))
	// apart from a function call's — see the space-before-"(" override below.
	lastKeyword := ""

	writeIndent := func(d int) {
		if d < 0 {
			d = 0
		}
		b.WriteString(strings.Repeat(" ", d*indentWidth))
	}
	newline := func(d int) {
		b.WriteByte('\n')
		writeIndent(d)
		atLineStart = true
		prev = nil
	}

	for i := range tokens {
		t := tokens[i]
		isClose := t.kind == tPunct && t.text == ")"
		if isClose {
			depth--
			if depth < 0 {
				depth = 0
			}
		}

		switch {
		case t.kind == tComment:
			if !atLineStart {
				newline(depth)
			}
			b.WriteString(t.text)
			newline(depth)
			continue
		case t.kind == tKeyword && lineKeywords[t.text]:
			if !atLineStart {
				newline(depth)
			}
		case t.kind == tKeyword && t.text == "ON":
			if !atLineStart {
				newline(depth + 1)
			}
		case isClose && closeParenOwnLine[i]:
			if !atLineStart {
				newline(depth)
			}
		default:
			if delta, ok := forceBreak[i]; ok {
				if !atLineStart {
					newline(depth + delta)
				}
			}
		}

		space := prev != nil && spaceBetween(*prev, t)
		if !space && prev != nil && t.kind == tPunct && t.text == "(" &&
			(prev.kind == tIdent || prev.kind == tQuoted) &&
			(lastKeyword == "INSERT INTO" || lastKeyword == "TABLE") {
			// t is a table name's column-list paren, not a function call's —
			// spaceBetween's default (ident immediately before "(" means a
			// call) is right far more often than it's wrong, so it's the
			// base rule; this is the narrow, safe exception.
			space = true
		}
		if !atLineStart && space {
			b.WriteByte(' ')
		}
		b.WriteString(displayText(t))
		atLineStart = false
		tCopy := t
		prev = &tCopy
		if t.kind == tKeyword {
			lastKeyword = t.text
		}

		if t.kind == tPunct && t.text == "(" {
			depth++
		}
	}

	return strings.TrimRight(b.String(), " \t\n")
}

// spaceBetween reports whether a space belongs between prev and cur when
// rendered inline. Punctuation that hugs its neighbor (",", ")", ";", ".",
// "::", and the "(" of a function call) suppresses it on one side or the
// other; everything else gets one.
func spaceBetween(prev, cur token) bool {
	if cur.kind == tPunct {
		switch cur.text {
		case ",", ")", ";", ".", "::":
			return false
		}
	}
	if prev.kind == tPunct {
		switch prev.text {
		case "(", ".", "::":
			return false
		}
	}
	if cur.kind == tPunct && cur.text == "(" && (prev.kind == tIdent || prev.kind == tQuoted) {
		return false // ident( — a function call, not "keyword ("
	}
	return true
}

func displayText(t token) string { return t.text }

// inlineLength is how long tokens would render as one space-joined line —
// the measure computeBreaks uses to decide whether a list is short enough to
// leave alone.
func inlineLength(tokens []token) int {
	n := 0
	var prev *token
	for i := range tokens {
		t := &tokens[i]
		if prev != nil && spaceBetween(*prev, *t) {
			n++
		}
		n += len([]rune(displayText(*t)))
		prev = t
	}
	return n
}
