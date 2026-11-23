package markdown

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/shnex/shnex/core/texmath"
)

// Document represents a compiled Markdown document with native Go-typeset vector math boxes.
// Shared across Desktop and future native Android (Jetpack Compose) with zero JS/WebView math runtime.
type Document struct {
	Outline []HeadingEntry `json:"outline"`
	Blocks  []Block        `json:"blocks"`
}

type HeadingEntry struct {
	ID    string `json:"id"`
	Level int    `json:"level"`
	Title string `json:"title"`
}

type Block struct {
	Kind      string                `json:"kind"` // heading, paragraph, math_block, code_block, blockquote, list, table, hr
	ID        string                `json:"id,omitempty"`
	Level     int                   `json:"level,omitempty"`
	Inlines   []Inline              `json:"inlines,omitempty"`
	Math      *texmath.RenderedMath `json:"math,omitempty"`
	Lang      string                `json:"lang,omitempty"`
	Code      string                `json:"code,omitempty"`
	AlertType string                `json:"alert_type,omitempty"` // NOTE, TIP, IMPORTANT, WARNING, CAUTION
	Ordered   bool                  `json:"ordered,omitempty"`
	Items     []ListItem            `json:"items,omitempty"`
	Aligns    []string              `json:"aligns,omitempty"` // left, center, right
	Headers   [][]Inline            `json:"headers,omitempty"`
	Rows      [][][]Inline          `json:"rows,omitempty"`
}

type ListItem struct {
	Checked *bool    `json:"checked,omitempty"`
	Inlines []Inline `json:"inlines"`
}

type Inline struct {
	Kind     string                `json:"kind"` // text, bold, italic, strike, code, link, image, math_inline
	Text     string                `json:"text,omitempty"`
	URL      string                `json:"url,omitempty"`
	Alt      string                `json:"alt,omitempty"`
	Children []Inline              `json:"children,omitempty"`
	Math     *texmath.RenderedMath `json:"math,omitempty"`
}

var (
	reHeading     = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	reHR          = regexp.MustCompile(`^\s*(?:-\s*-\s*-[- ]*|\*\s*\*\s*\*[* ]*|_\s*_\s*_[_ ]*)\s*$`)
	reOrderedItem = regexp.MustCompile(`^\s*\d+[.)]\s+(.+)$`)
	reBulletItem  = regexp.MustCompile(`^\s*[-*+]\s+(.+)$`)
	reAlertHeader = regexp.MustCompile(`^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*(.*)$`)
)

// Compile parses Markdown source text and typesets all inline & display TeX formulas natively in Go.
func Compile(src string) Document {
	norm := strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(norm, "\n")

	var blocks []Block
	var outline []HeadingEntry
	headingSeq := 0

	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			i++
			continue
		}

		// 1. Fenced Code Block (``` or ~~~)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence := trimmed[:3]
			lang := strings.TrimSpace(trimmed[3:])
			i++
			var codeLines []string
			for i < len(lines) {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
					i++
					break
				}
				codeLines = append(codeLines, lines[i])
				i++
			}
			blocks = append(blocks, Block{
				Kind: "code_block",
				Lang: lang,
				Code: strings.Join(codeLines, "\n"),
			})
			continue
		}

		// 2. Display Math Block: $$ ... $$ or \[ ... \]
		if strings.HasPrefix(trimmed, "$$") || strings.HasPrefix(trimmed, `\[`) {
			openTok := "$$"
			closeTok := "$$"
			if strings.HasPrefix(trimmed, `\[`) {
				openTok = `\[`
				closeTok = `\]`
			}
			rest := strings.TrimSpace(trimmed[len(openTok):])
			if rest != "" && strings.HasSuffix(rest, closeTok) && len(rest) >= len(closeTok) {
				texBody := strings.TrimSpace(rest[:len(rest)-len(closeTok)])
				rendered := texmath.Render(texBody, true, 18.5)
				blocks = append(blocks, Block{Kind: "math_block", Math: &rendered})
				i++
				continue
			}
			var mathLines []string
			if rest != "" {
				mathLines = append(mathLines, rest)
			}
			i++
			for i < len(lines) {
				tLine := strings.TrimSpace(lines[i])
				if strings.HasSuffix(tLine, closeTok) {
					before := strings.TrimSpace(tLine[:len(tLine)-len(closeTok)])
					if before != "" {
						mathLines = append(mathLines, before)
					}
					i++
					break
				}
				mathLines = append(mathLines, lines[i])
				i++
			}
			rendered := texmath.Render(strings.Join(mathLines, "\n"), true, 18.5)
			blocks = append(blocks, Block{Kind: "math_block", Math: &rendered})
			continue
		}

		// 3. Heading (# .. ######)
		if m := reHeading.FindStringSubmatch(line); m != nil {
			lvl := len(m[1])
			rawTitle := strings.TrimRight(m[2], " #")
			headingSeq++
			id := fmt.Sprintf("sec-%d", headingSeq)
			inls := parseInlines(rawTitle)
			plainTitle := plainTextFromInlines(inls)
			outline = append(outline, HeadingEntry{
				ID:    id,
				Level: lvl,
				Title: plainTitle,
			})
			blocks = append(blocks, Block{
				Kind:    "heading",
				ID:      id,
				Level:   lvl,
				Inlines: inls,
			})
			i++
			continue
		}

		// 4. Horizontal Rule (---, ***)
		if reHR.MatchString(line) {
			blocks = append(blocks, Block{Kind: "hr"})
			i++
			continue
		}

		// 5. GFM Table (header row followed by | --- | separator row)
		if strings.Contains(line, "|") && i+1 < len(lines) && isTableSeparatorRow(lines[i+1]) {
			headersRaw := splitTableRow(line)
			aligns := parseTableAligns(lines[i+1], len(headersRaw))
			headers := make([][]Inline, len(headersRaw))
			for ci, cell := range headersRaw {
				headers[ci] = parseInlines(cell)
			}
			i += 2
			var rows [][][]Inline
			for i < len(lines) {
				rowTrim := strings.TrimSpace(lines[i])
				if rowTrim == "" || !strings.Contains(rowTrim, "|") {
					break
				}
				cellsRaw := splitTableRow(lines[i])
				row := make([][]Inline, len(headers))
				for ci := range headers {
					if ci < len(cellsRaw) {
						row[ci] = parseInlines(cellsRaw[ci])
					}
				}
				rows = append(rows, row)
				i++
			}
			blocks = append(blocks, Block{
				Kind:    "table",
				Aligns:  aligns,
				Headers: headers,
				Rows:    rows,
			})
			continue
		}

		// 6. Blockquote / GitHub Alert (> ...)
		if strings.HasPrefix(trimmed, ">") {
			var qLines []string
			for i < len(lines) {
				qt := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(qt, ">") {
					break
				}
				content := strings.TrimPrefix(qt, ">")
				content = strings.TrimPrefix(content, " ")
				qLines = append(qLines, content)
				i++
			}
			alertType := ""
			if len(qLines) > 0 {
				if am := reAlertHeader.FindStringSubmatch(strings.TrimSpace(qLines[0])); am != nil {
					alertType = am[1]
					if strings.TrimSpace(am[2]) != "" {
						qLines[0] = strings.TrimSpace(am[2])
					} else {
						qLines = qLines[1:]
					}
				}
			}
			blocks = append(blocks, Block{
				Kind:      "blockquote",
				AlertType: alertType,
				Inlines:   parseInlines(strings.Join(qLines, " ")),
			})
			continue
		}

		// 7. Ordered / Unordered / Task Lists
		if reBulletItem.MatchString(line) || reOrderedItem.MatchString(line) {
			isOrd := reOrderedItem.MatchString(line)
			var items []ListItem
			for i < len(lines) {
				var body string
				if isOrd {
					m := reOrderedItem.FindStringSubmatch(lines[i])
					if m == nil {
						break
					}
					body = m[1]
				} else {
					m := reBulletItem.FindStringSubmatch(lines[i])
					if m == nil {
						break
					}
					body = m[1]
				}
				var checked *bool
				if strings.HasPrefix(body, "[x] ") || strings.HasPrefix(body, "[X] ") {
					t := true
					checked = &t
					body = body[4:]
				} else if strings.HasPrefix(body, "[ ] ") {
					f := false
					checked = &f
					body = body[4:]
				}
				items = append(items, ListItem{
					Checked: checked,
					Inlines: parseInlines(strings.TrimSpace(body)),
				})
				i++
			}
			blocks = append(blocks, Block{
				Kind:    "list",
				Ordered: isOrd,
				Items:   items,
			})
			continue
		}

		// 8. Paragraph (collect contiguous normal lines until blank or block starter)
		var paraLines []string
		for i < len(lines) {
			lt := strings.TrimSpace(lines[i])
			if lt == "" ||
				strings.HasPrefix(lt, "```") ||
				strings.HasPrefix(lt, "~~~") ||
				strings.HasPrefix(lt, "$$") ||
				strings.HasPrefix(lt, `\[`) ||
				strings.HasPrefix(lt, ">") ||
				reHeading.MatchString(lines[i]) ||
				reHR.MatchString(lines[i]) ||
				reBulletItem.MatchString(lines[i]) ||
				reOrderedItem.MatchString(lines[i]) ||
				(strings.Contains(lines[i], "|") && i+1 < len(lines) && isTableSeparatorRow(lines[i+1])) {
				break
			}
			paraLines = append(paraLines, lt)
			i++
		}
		if len(paraLines) > 0 {
			blocks = append(blocks, Block{
				Kind:    "paragraph",
				Inlines: parseInlines(strings.Join(paraLines, " ")),
			})
		}
	}

	return Document{
		Outline: outline,
		Blocks:  blocks,
	}
}

func isTableSeparatorRow(line string) bool {
	s := strings.TrimSpace(line)
	if !strings.Contains(s, "|") || !strings.Contains(s, "-") {
		return false
	}
	cells := splitTableRow(s)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		c = strings.TrimPrefix(c, ":")
		c = strings.TrimSuffix(c, ":")
		if len(c) == 0 || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}

func splitTableRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	var cells []string
	var cur strings.Builder
	inMath := false
	inCode := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\\' && i+1 < len(s) {
			cur.WriteByte(ch)
			cur.WriteByte(s[i+1])
			i++
			continue
		}
		if ch == '`' {
			inCode = !inCode
		} else if ch == '$' && !inCode {
			inMath = !inMath
		}
		if ch == '|' && !inMath && !inCode {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

func parseTableAligns(sepLine string, count int) []string {
	raw := splitTableRow(sepLine)
	aligns := make([]string, count)
	for i := 0; i < count; i++ {
		aligns[i] = "left"
		if i < len(raw) {
			c := strings.TrimSpace(raw[i])
			l := strings.HasPrefix(c, ":")
			r := strings.HasSuffix(c, ":")
			if l && r {
				aligns[i] = "center"
			} else if r {
				aligns[i] = "right"
			}
		}
	}
	return aligns
}

func plainTextFromInlines(inls []Inline) string {
	var b strings.Builder
	for _, in := range inls {
		if in.Text != "" {
			b.WriteString(in.Text)
		}
		if in.Math != nil {
			b.WriteString(in.Math.TeX)
		}
		if len(in.Children) > 0 {
			b.WriteString(plainTextFromInlines(in.Children))
		}
	}
	return strings.TrimSpace(b.String())
}

// parseInlines tokenizes inline Markdown formatting + native inline TeX math ($...$ and \(...\)).
func parseInlines(s string) []Inline {
	var out []Inline
	var buf strings.Builder

	flushText := func() {
		if buf.Len() > 0 {
			out = append(out, Inline{Kind: "text", Text: buf.String()})
			buf.Reset()
		}
	}

	i := 0
	for i < len(s) {
		// 1. Escaped punctuation (\$ \* \_ \` \[ \])
		if s[i] == '\\' && i+1 < len(s) {
			next := s[i+1]
			if next == '(' {
				// Inline math \( ... \)
				if end := strings.Index(s[i+2:], `\)`); end >= 0 {
					flushText()
					tex := strings.TrimSpace(s[i+2 : i+2+end])
					rendered := texmath.Render(tex, false, 16.0)
					out = append(out, Inline{Kind: "math_inline", Math: &rendered})
					i += 2 + end + 2
					continue
				}
			}
			if strings.ContainsRune(`\$*_`+"`[]()~|#", rune(next)) {
				buf.WriteByte(next)
				i += 2
				continue
			}
		}

		// 2. Inline code `...`
		if s[i] == '`' {
			end := strings.IndexByte(s[i+1:], '`')
			if end >= 0 {
				flushText()
				out = append(out, Inline{Kind: "code", Text: s[i+1 : i+1+end]})
				i += 1 + end + 1
				continue
			}
		}

		// 3. Inline math $...$ (single $, non-empty, not starting/ending with space)
		if s[i] == '$' && (i+1 < len(s) && s[i+1] != '$' && s[i+1] != ' ' && s[i+1] != '\t') {
			end := findClosingInlineDollar(s, i+1)
			if end > i+1 {
				flushText()
				tex := s[i+1 : end]
				rendered := texmath.Render(tex, false, 16.0)
				out = append(out, Inline{Kind: "math_inline", Math: &rendered})
				i = end + 1
				continue
			}
		}

		// 4. Image ![alt](url)
		if strings.HasPrefix(s[i:], "![") {
			if closeBr := strings.IndexByte(s[i+2:], ']'); closeBr >= 0 {
				afterBr := i + 2 + closeBr + 1
				if afterBr < len(s) && s[afterBr] == '(' {
					if closeParen := strings.IndexByte(s[afterBr+1:], ')'); closeParen >= 0 {
						flushText()
						alt := s[i+2 : i+2+closeBr]
						url := strings.TrimSpace(s[afterBr+1 : afterBr+1+closeParen])
						out = append(out, Inline{Kind: "image", Alt: alt, URL: url})
						i = afterBr + 1 + closeParen + 1
						continue
					}
				}
			}
		}

		// 5. Link [label](url)
		if s[i] == '[' {
			if closeBr := strings.IndexByte(s[i+1:], ']'); closeBr >= 0 {
				afterBr := i + 1 + closeBr + 1
				if afterBr < len(s) && s[afterBr] == '(' {
					if closeParen := strings.IndexByte(s[afterBr+1:], ')'); closeParen >= 0 {
						flushText()
						lbl := s[i+1 : i+1+closeBr]
						url := strings.TrimSpace(s[afterBr+1 : afterBr+1+closeParen])
						out = append(out, Inline{
							Kind:     "link",
							URL:      url,
							Children: parseInlines(lbl),
						})
						i = afterBr + 1 + closeParen + 1
						continue
					}
				}
			}
		}

		// 6. Bold **...**
		if strings.HasPrefix(s[i:], "**") {
			if end := strings.Index(s[i+2:], "**"); end > 0 {
				flushText()
				out = append(out, Inline{
					Kind:     "bold",
					Children: parseInlines(s[i+2 : i+2+end]),
				})
				i += 2 + end + 2
				continue
			}
		}

		// 7. Strikethrough ~~...~~
		if strings.HasPrefix(s[i:], "~~") {
			if end := strings.Index(s[i+2:], "~~"); end > 0 {
				flushText()
				out = append(out, Inline{
					Kind:     "strike",
					Children: parseInlines(s[i+2 : i+2+end]),
				})
				i += 2 + end + 2
				continue
			}
		}

		// 8. Italic *...*
		if s[i] == '*' && i+1 < len(s) && s[i+1] != ' ' {
			if end := strings.IndexByte(s[i+1:], '*'); end > 0 {
				flushText()
				out = append(out, Inline{
					Kind:     "italic",
					Children: parseInlines(s[i+1 : i+1+end]),
				})
				i += 1 + end + 1
				continue
			}
		}

		buf.WriteByte(s[i])
		i++
	}

	flushText()
	return out
}

func findClosingInlineDollar(s string, start int) int {
	for idx := start; idx < len(s); idx++ {
		if s[idx] == '\\' {
			idx++
			continue
		}
		if s[idx] == '$' {
			if s[idx-1] == ' ' || s[idx-1] == '\t' {
				return -1
			}
			return idx
		}
	}
	return -1
}
