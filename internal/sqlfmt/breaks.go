package sqlfmt

// computeBreaks is the printer's one lookahead pass: it decides, before any
// output is written, which SELECT/INSERT list items and VALUES tuples get
// their own line. Line breaking on clause keywords (SELECT, FROM, WHERE, …)
// needs no lookahead — it's unconditional — but a list only breaks past
// listBreakWidth, which can only be known by scanning the whole list first.
//
// forceBreak maps a token index to an indent delta: formatStatement, on
// reaching that index, breaks to a new line at (current depth + delta)
// before emitting it — used for both a list's first item (right after the
// opening keyword/paren) and every item after a top-level comma.
// closeParenOwnLine marks a ")" that closes a broken paren list, so the
// closing paren lands on its own line at the outer depth instead of hugging
// the last item.
func computeBreaks(tokens []token) (forceBreak map[int]int, closeParenOwnLine map[int]bool) {
	forceBreak = map[int]int{}
	closeParenOwnLine = map[int]bool{}

	depth := 0
	for i, t := range tokens {
		switch {
		case t.kind == tPunct && t.text == "(":
			depth++
		case t.kind == tPunct && t.text == ")":
			depth--
		case t.kind == tKeyword && t.text == "SELECT":
			markSelectList(tokens, i, depth, forceBreak)
		case t.kind == tKeyword && t.text == "INSERT INTO":
			markInsertColumnList(tokens, i, forceBreak, closeParenOwnLine)
		case t.kind == tKeyword && t.text == "VALUES":
			markValuesTuples(tokens, i, depth, forceBreak)
		}
	}
	return forceBreak, closeParenOwnLine
}

// listSpan finds the end of a keyword-delimited list starting at start
// (which is inside the statement at the given depth): the index of the next
// token that is either a lineKeyword at the same depth, a top-level ';', a
// ")" that would close past depth, or the end of the token stream.
func listSpan(tokens []token, start, depth int) int {
	d := depth
	end := start
	for end < len(tokens) {
		tk := tokens[end]
		switch {
		case tk.kind == tPunct && tk.text == "(":
			d++
		case tk.kind == tPunct && tk.text == ")":
			if d == depth {
				return end
			}
			d--
		case d == depth && tk.kind == tKeyword && lineKeywords[tk.text]:
			return end
		case d == 0 && tk.kind == tPunct && tk.text == ";":
			return end
		}
		end++
	}
	return end
}

// topLevelCommas returns the indices, within [start, end), of every comma at
// exactly depth d — i.e. a list separator, not a comma inside a nested
// function call or subquery.
func topLevelCommas(tokens []token, start, end, d int) []int {
	var idxs []int
	depth := d
	for k := start; k < end; k++ {
		tk := tokens[k]
		switch {
		case tk.kind == tPunct && tk.text == "(":
			depth++
		case tk.kind == tPunct && tk.text == ")":
			depth--
		case depth == d && tk.kind == tPunct && tk.text == ",":
			idxs = append(idxs, k)
		}
	}
	return idxs
}

// markSelectList marks a SELECT's column list, if it's worth breaking: past
// listBreakWidth rendered inline, and more than one column (a single
// expression — even a long one — has nowhere useful to break).
func markSelectList(tokens []token, selIdx, selDepth int, forceBreak map[int]int) {
	start := selIdx + 1
	if start < len(tokens) && tokens[start].kind == tKeyword && tokens[start].text == "DISTINCT" {
		start++
	}
	end := listSpan(tokens, start, selDepth)
	if start >= end {
		return
	}
	commas := topLevelCommas(tokens, start, end, selDepth)
	if len(commas) == 0 || inlineLength(tokens[start:end]) <= listBreakWidth {
		return
	}
	forceBreak[start] = 1
	for _, ci := range commas {
		if ci+1 < len(tokens) {
			forceBreak[ci+1] = 1
		}
	}
}

// markInsertColumnList marks the parenthesized column list of an
// "INSERT INTO table (a, b, c)" — found by skipping the target table name
// (and any schema-qualifying dots) right after the INSERT INTO keyword and
// looking for the paren that follows it.
func markInsertColumnList(tokens []token, insertIdx int, forceBreak map[int]int, closeParenOwnLine map[int]bool) {
	i := insertIdx + 1
	for i < len(tokens) {
		t := tokens[i]
		if t.kind == tIdent || t.kind == tQuoted || (t.kind == tPunct && t.text == ".") {
			i++
			continue
		}
		break
	}
	if i >= len(tokens) || !(tokens[i].kind == tPunct && tokens[i].text == "(") {
		return
	}
	openIdx := i
	depth := 0
	closeIdx := -1
	for j := openIdx; j < len(tokens); j++ {
		switch {
		case tokens[j].kind == tPunct && tokens[j].text == "(":
			depth++
		case tokens[j].kind == tPunct && tokens[j].text == ")":
			depth--
			if depth == 0 {
				closeIdx = j
			}
		}
		if closeIdx != -1 {
			break
		}
	}
	if closeIdx == -1 {
		return
	}
	start := openIdx + 1
	if start >= closeIdx {
		return
	}
	if inlineLength(tokens[start:closeIdx]) <= listBreakWidth {
		return
	}
	forceBreak[start] = 0
	for _, ci := range topLevelCommas(tokens, start, closeIdx, 1) {
		if ci+1 < len(tokens) {
			forceBreak[ci+1] = 0
		}
	}
	closeParenOwnLine[closeIdx] = true
}

// markValuesTuples marks the commas *between* VALUES tuples — "(1, 2),
// (3, 4)" — so each row lands on its own line whenever there's more than
// one. Unlike the column lists above, this doesn't gate on width: a
// multi-row INSERT is the readability problem regardless of how short each
// row is.
func markValuesTuples(tokens []token, valuesIdx, valuesDepth int, forceBreak map[int]int) {
	start := valuesIdx + 1
	end := listSpan(tokens, start, valuesDepth)
	for _, ci := range topLevelCommas(tokens, start, end, valuesDepth) {
		if ci+1 < len(tokens) {
			forceBreak[ci+1] = 1
		}
	}
}
