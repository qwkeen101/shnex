package completion

import (
	"strings"
	"unicode/utf8"
)

type parsedLine struct {
	args           []string // completed arguments before the active token
	activeRaw      string   // exact substring of the active token as typed (including any leading quote)
	activeUnquoted string   // unquoted text of the active token for matching
	quoteChar      byte     // 0, '\'', or '"'
	hasTrailingSp  bool     // true if cursor sits after whitespace starting a brand-new empty token
}

// parseActiveCommandSegment extracts the last pipeline/chain command segment (`|`, `&&`, `||`, `;`)
// and splits completed tokens from the active token right before the cursor.
func parseActiveCommandSegment(line string) parsedLine {
	seg := extractTailSegment(line)
	seg = strings.TrimLeft(seg, " \t")
	if seg == "" {
		return parsedLine{}
	}

	var args []string
	var cur strings.Builder
	var rawCur strings.Builder
	var inQuote byte
	escaped := false

	for i := 0; i < len(seg); i++ {
		ch := seg[i]
		if escaped {
			cur.WriteByte(ch)
			rawCur.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' && inQuote != '\'' {
			// Note: on Windows paths like .\foo or C:\bar, backslash is a path separator unless escaping space
			if i+1 < len(seg) && (seg[i+1] == ' ' || seg[i+1] == '"' || seg[i+1] == '\'') {
				escaped = true
				rawCur.WriteByte(ch)
				continue
			}
		}
		if inQuote != 0 {
			rawCur.WriteByte(ch)
			if ch == inQuote {
				inQuote = 0
			} else {
				cur.WriteByte(ch)
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			inQuote = ch
			rawCur.WriteByte(ch)
			continue
		}
		if ch == ' ' || ch == '\t' {
			if rawCur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
				rawCur.Reset()
			}
			continue
		}
		cur.WriteByte(ch)
		rawCur.WriteByte(ch)
	}

	if rawCur.Len() == 0 && len(args) > 0 && (strings.HasSuffix(seg, " ") || strings.HasSuffix(seg, "\t")) {
		return parsedLine{
			args:          args,
			hasTrailingSp: true,
		}
	}

	return parsedLine{
		args:           args,
		activeRaw:      rawCur.String(),
		activeUnquoted: cur.String(),
		quoteChar:      inQuote,
	}
}

func extractTailSegment(line string) string {
	var inQuote byte
	lastCut := 0
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inQuote != 0 {
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			inQuote = ch
			continue
		}
		if ch == ';' || ch == '|' {
			lastCut = i + 1
			continue
		}
		if ch == '&' && i+1 < len(line) && line[i+1] == '&' {
			lastCut = i + 2
			i++
			continue
		}
	}
	return line[lastCut:]
}

// buildShellSafeCompletion computes InsertText, GhostSuffix, and ReplaceLen (in runes)
// handling spaces in filenames cleanly across PowerShell and POSIX shells.
func buildShellSafeCompletion(activeRaw, activeUnquoted string, quoteChar byte, fullTarget string, appendSpace bool, isDir bool) (insertText string, ghostSuffix string, replaceLen int) {
	replaceLen = utf8.RuneCountInString(activeRaw)
	needsQuote := strings.ContainsAny(fullTarget, " \t()[]{}&;")

	suffixSep := ""
	if isDir {
		if !strings.HasSuffix(fullTarget, "/") && !strings.HasSuffix(fullTarget, `\`) {
			suffixSep = "/"
		}
	} else if appendSpace {
		suffixSep = " "
	}

	rawCompletion := fullTarget + suffixSep

	if quoteChar != 0 {
		q := string(quoteChar)
		if isDir {
			// Keep quote open while navigating directories
			insertText = q + fullTarget + suffixSep
		} else {
			insertText = q + fullTarget + q + suffixSep
		}
	} else if needsQuote {
		// Single quotes work identically in PowerShell, Bash, Zsh, and Fish!
		escaped := strings.ReplaceAll(fullTarget, "'", "''")
		if isDir {
			insertText = "'" + escaped + suffixSep
		} else {
			insertText = "'" + escaped + "'" + suffixSep
		}
	} else {
		insertText = rawCompletion
	}

	// Compute crisp inline ghost text right after cursor
	if strings.HasPrefix(strings.ToLower(insertText), strings.ToLower(activeRaw)) {
		// Exact prefix continuation (including case-insensitive match on Windows/Git)
		rawRunes := []rune(activeRaw)
		insRunes := []rune(insertText)
		if len(insRunes) >= len(rawRunes) {
			ghostSuffix = string(insRunes[len(rawRunes):])
		}
	} else if activeUnquoted != "" && strings.HasPrefix(strings.ToLower(fullTarget), strings.ToLower(activeUnquoted)) {
		// Unquoted input matching a filename with spaces (e.g. typed `cat my`, target `my report.txt`)
		uRunes := []rune(activeUnquoted)
		tRunes := []rune(fullTarget)
		if len(tRunes) >= len(uRunes) {
			ghostSuffix = string(tRunes[len(uRunes):]) + suffixSep
		}
	} else if activeRaw == "" {
		ghostSuffix = insertText
	}

	return insertText, ghostSuffix, replaceLen
}
