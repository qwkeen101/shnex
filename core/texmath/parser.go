package texmath

import (
	"strings"
	"unicode"
)

// Node is a syntax tree node in our native TeX formula parser.
type Node interface {
	atomClass() AtomClass
}

type RowNode struct {
	Items []Node
}

func (r *RowNode) atomClass() AtomClass {
	if len(r.Items) == 1 {
		return r.Items[0].atomClass()
	}
	return AtomOrd
}

type SymbolNode struct {
	Text   string
	Italic bool
	Bold   bool
	Class  AtomClass
}

func (s *SymbolNode) atomClass() AtomClass { return s.Class }

type SpaceNode struct {
	Mu float64
}

func (s *SpaceNode) atomClass() AtomClass { return AtomOrd }

type BigOpNode struct {
	Kind          string // vector op ("sum", "int", "iint", "oint", ...) or named op ("lim", "sin", ...)
	IsVector      bool
	DefaultLimits bool
}

func (b *BigOpNode) atomClass() AtomClass { return AtomOp }

type ScriptsNode struct {
	Base        Node
	Sub         Node
	Sup         Node
	ForceLimits *bool
}

func (s *ScriptsNode) atomClass() AtomClass {
	if s.Base != nil {
		return s.Base.atomClass()
	}
	return AtomOrd
}

type FracNode struct {
	Num        Node
	Den        Node
	HasBar     bool
	ForceStyle *MathStyle
	WrapParen  bool // \binom{n}{k}
}

func (f *FracNode) atomClass() AtomClass { return AtomInner }

type SqrtNode struct {
	Degree Node
	Body   Node
}

func (s *SqrtNode) atomClass() AtomClass { return AtomOrd }

type DelimitedNode struct {
	Left  string
	Right string
	Parts []*RowNode // separated by \middle| if present
	Mids  []string
}

func (d *DelimitedNode) atomClass() AtomClass { return AtomInner }

type AccentNode struct {
	Kind string // "vec", "overrightarrow", "hat", "bar", "overline", "underline", "tilde", "dot", "ddot"
	Body Node
}

func (a *AccentNode) atomClass() AtomClass { return AtomOrd }

type MatrixNode struct {
	Env  string
	Rows [][]*RowNode
}

func (m *MatrixNode) atomClass() AtomClass { return AtomInner }

type TextRunNode struct {
	Text   string
	Bold   bool
	Italic bool
}

func (t *TextRunNode) atomClass() AtomClass { return AtomOrd }

// Parse parses a LaTeX math expression into an AST root *RowNode.
func Parse(src string) *RowNode {
	p := &parser{src: strings.TrimSpace(src)}
	return p.parseRow(stopNone)
}

type stopCondition int

const (
	stopNone stopCondition = iota
	stopRightBrace
	stopRightDelim
	stopMatrixCellOrEnd
)

type parser struct {
	src string
	pos int
}

func (p *parser) eof() bool {
	return p.pos >= len(p.src)
}

func (p *parser) skipSpaces() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			p.pos++
		} else if c == '%' {
			// TeX comment to end of line
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		} else {
			break
		}
	}
}

func (p *parser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) hasCommandAhead(cmd string) bool {
	save := p.pos
	p.skipSpaces()
	ok := strings.HasPrefix(p.src[p.pos:], cmd)
	if ok {
		end := p.pos + len(cmd)
		if end < len(p.src) && isASCIIAlpha(p.src[end]) {
			ok = false
		}
	}
	p.pos = save
	return ok
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func (p *parser) parseRow(stop stopCondition) *RowNode {
	var items []Node
	for {
		p.skipSpaces()
		if p.eof() {
			break
		}
		ch := p.peek()
		if stop == stopRightBrace && ch == '}' {
			p.pos++
			break
		}
		if stop == stopRightDelim && (p.hasCommandAhead(`\right`) || p.hasCommandAhead(`\middle`)) {
			break
		}
		if stop == stopMatrixCellOrEnd {
			if ch == '&' || strings.HasPrefix(p.src[p.pos:], `\\`) || p.hasCommandAhead(`\end`) {
				break
			}
		}
		if ch == '}' {
			// Unmatched closing brace: consume safely without infinite loop
			p.pos++
			break
		}

		n := p.parseScriptedAtom()
		if n != nil {
			items = append(items, n)
		}
	}
	return &RowNode{Items: items}
}

func (p *parser) parseScriptedAtom() Node {
	base := p.parseAtom()
	if base == nil {
		return nil
	}

	var sub, sup Node
	var forceLimits *bool

	for {
		p.skipSpaces()
		if p.eof() {
			break
		}
		if p.hasCommandAhead(`\limits`) {
			p.skipSpaces()
			p.pos += len(`\limits`)
			t := true
			forceLimits = &t
			continue
		}
		if p.hasCommandAhead(`\nolimits`) {
			p.skipSpaces()
			p.pos += len(`\nolimits`)
			f := false
			forceLimits = &f
			continue
		}
		c := p.peek()
		if c == '_' && sub == nil {
			p.pos++
			sub = p.parseAtom()
			continue
		}
		if c == '^' && sup == nil {
			p.pos++
			sup = p.parseAtom()
			continue
		}
		if c == '\'' {
			// Prime shorthand: f'(x), f''(x)
			primes := ""
			for !p.eof() && p.peek() == '\'' {
				primes += "′"
				p.pos++
			}
			primeSym := &SymbolNode{Text: primes, Class: AtomOrd}
			if sup == nil {
				sup = primeSym
			} else {
				sup = &RowNode{Items: []Node{sup, primeSym}}
			}
			continue
		}
		break
	}

	if sub == nil && sup == nil && forceLimits == nil {
		return base
	}
	return &ScriptsNode{
		Base:        base,
		Sub:         sub,
		Sup:         sup,
		ForceLimits: forceLimits,
	}
}

func (p *parser) parseAtom() Node {
	p.skipSpaces()
	if p.eof() {
		return nil
	}
	ch := p.peek()
	if ch == '{' {
		p.pos++
		return p.parseRow(stopRightBrace)
	}
	if ch == '\\' {
		return p.parseCommand()
	}

	// Decode UTF-8 rune
	r, width := decodeRuneAt(p.src, p.pos)
	p.pos += width
	return classifyLiteralRune(r)
}

func decodeRuneAt(s string, idx int) (rune, int) {
	for _, r := range s[idx:] {
		return r, len(string(r))
	}
	return 0, 1
}

func classifyLiteralRune(r rune) Node {
	switch r {
	case '+', '-', '±', '×', '÷':
		if r == '-' {
			return &SymbolNode{Text: "−", Class: AtomBin}
		}
		return &SymbolNode{Text: string(r), Class: AtomBin}
	case '=', '<', '>', '≤', '≥', '≠', '≈', '≡', '→', '←', '∈':
		return &SymbolNode{Text: string(r), Class: AtomRel}
	case '(', '[', '⟨':
		return &SymbolNode{Text: string(r), Class: AtomOpen}
	case ')', ']', '⟩', '!', '?':
		return &SymbolNode{Text: string(r), Class: AtomClose}
	case ',', ';', ':':
		if r == ':' {
			return &SymbolNode{Text: ":", Class: AtomRel}
		}
		return &SymbolNode{Text: string(r), Class: AtomPunct}
	case '|':
		return &SymbolNode{Text: "|", Class: AtomOrd}
	default:
		italic := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		return &SymbolNode{Text: string(r), Italic: italic, Class: AtomOrd}
	}
}

func (p *parser) readCommandName() string {
	if p.eof() || p.src[p.pos] != '\\' {
		return ""
	}
	p.pos++ // skip '\'
	if p.eof() {
		return ""
	}
	first := p.src[p.pos]
	if !isASCIIAlpha(first) {
		p.pos++
		return string(first)
	}
	start := p.pos
	for p.pos < len(p.src) && isASCIIAlpha(p.src[p.pos]) {
		p.pos++
	}
	// Optional star suffix e.g. \operatorname*
	return p.src[start:p.pos]
}

func (p *parser) readBracedRawText() string {
	p.skipSpaces()
	if p.eof() {
		return ""
	}
	if p.peek() != '{' {
		r, w := decodeRuneAt(p.src, p.pos)
		p.pos += w
		return string(r)
	}
	p.pos++ // skip '{'
	depth := 1
	start := p.pos
	for p.pos < len(p.src) && depth > 0 {
		switch p.src[p.pos] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				out := p.src[start:p.pos]
				p.pos++
				return out
			}
		case '\\':
			if p.pos+1 < len(p.src) {
				p.pos++
			}
		}
		p.pos++
	}
	return p.src[start:p.pos]
}

func (p *parser) readBracketOptional() Node {
	p.skipSpaces()
	if p.eof() || p.peek() != '[' {
		return nil
	}
	p.pos++ // skip '['
	var items []Node
	for !p.eof() {
		p.skipSpaces()
		if p.eof() {
			break
		}
		if p.peek() == ']' {
			p.pos++
			break
		}
		n := p.parseScriptedAtom()
		if n != nil {
			items = append(items, n)
		}
	}
	return &RowNode{Items: items}
}

func (p *parser) readDelimiterToken() string {
	p.skipSpaces()
	if p.eof() {
		return "."
	}
	if p.peek() == '\\' {
		cmd := p.readCommandName()
		switch cmd {
		case "{", "lbrace":
			return "{"
		case "}", "rbrace":
			return "}"
		case "|", "Vert", "lVert", "rVert":
			return "‖"
		case "vert", "lvert", "rvert":
			return "|"
		case "langle":
			return "⟨"
		case "rangle":
			return "⟩"
		case "lfloor":
			return "⌊"
		case "rfloor":
			return "⌋"
		case "lceil":
			return "⌈"
		case "rceil":
			return "⌉"
		default:
			return cmd
		}
	}
	r, w := decodeRuneAt(p.src, p.pos)
	p.pos += w
	return string(r)
}

func (p *parser) parseCommand() Node {
	cmd := p.readCommandName()
	switch cmd {
	case "{", "lbrace":
		return &SymbolNode{Text: "{", Class: AtomOpen}
	case "}", "rbrace":
		return &SymbolNode{Text: "}", Class: AtomClose}
	case "|", "Vert", "lVert", "rVert":
		return &SymbolNode{Text: "‖", Class: AtomOrd}
	case "vert", "lvert", "rvert":
		return &SymbolNode{Text: "|", Class: AtomOrd}
	case "%", "$", "&", "_", "#":
		return &SymbolNode{Text: cmd, Class: AtomOrd}
	case "backslash":
		return &SymbolNode{Text: `\`, Class: AtomOrd}

	case "frac", "dfrac", "tfrac", "cfrac":
		num := p.parseAtom()
		den := p.parseAtom()
		var force *MathStyle
		if cmd == "dfrac" || cmd == "cfrac" {
			st := StyleDisplay
			force = &st
		} else if cmd == "tfrac" {
			st := StyleText
			force = &st
		}
		return &FracNode{Num: num, Den: den, HasBar: true, ForceStyle: force}

	case "binom", "dbinom", "tbinom":
		num := p.parseAtom()
		den := p.parseAtom()
		return &FracNode{Num: num, Den: den, HasBar: false, WrapParen: true}

	case "sqrt":
		deg := p.readBracketOptional()
		body := p.parseAtom()
		return &SqrtNode{Degree: deg, Body: body}

	case "left":
		leftDelim := p.readDelimiterToken()
		var parts []*RowNode
		var mids []string
		for {
			part := p.parseRow(stopRightDelim)
			parts = append(parts, part)
			if p.hasCommandAhead(`\middle`) {
				p.skipSpaces()
				p.pos += len(`\middle`)
				mids = append(mids, p.readDelimiterToken())
				continue
			}
			break
		}
		rightDelim := "."
		if p.hasCommandAhead(`\right`) {
			p.skipSpaces()
			p.pos += len(`\right`)
			rightDelim = p.readDelimiterToken()
		}
		return &DelimitedNode{
			Left:  leftDelim,
			Right: rightDelim,
			Parts: parts,
			Mids:  mids,
		}

	case "begin":
		env := strings.TrimSpace(p.readBracedRawText())
		return p.parseEnvironment(env)

	case "vec", "overrightarrow", "hat", "widehat", "bar", "overline", "underline", "tilde", "widetilde", "dot", "ddot":
		kind := cmd
		if kind == "widehat" {
			kind = "hat"
		} else if kind == "widetilde" {
			kind = "tilde"
		}
		body := p.parseAtom()
		return &AccentNode{Kind: kind, Body: body}

	case "text", "textrm", "mathrm", "textbf", "mathbf", "mathit", "mathbb", "mathcal", "operatorname":
		hasStar := false
		if cmd == "operatorname" && !p.eof() && p.peek() == '*' {
			hasStar = true
			p.pos++
		}
		raw := p.readBracedRawText()
		switch cmd {
		case "mathbb":
			return &SymbolNode{Text: toBlackboardBold(raw), Class: AtomOrd}
		case "operatorname":
			return &BigOpNode{Kind: raw, IsVector: false, DefaultLimits: hasStar}
		case "textbf", "mathbf":
			return &TextRunNode{Text: raw, Bold: true}
		case "mathit":
			return &TextRunNode{Text: raw, Italic: true}
		case "mathcal":
			return &TextRunNode{Text: raw, Italic: true, Bold: true}
		default:
			return &TextRunNode{Text: raw}
		}

	case "displaystyle", "textstyle", "scriptstyle", "scriptscriptstyle", "limits", "nolimits":
		return &SpaceNode{Mu: 0}
	}

	if mu, ok := spaceCommandsMu[cmd]; ok {
		return &SpaceNode{Mu: mu}
	}
	if defLimits, ok := vectorBigOps[cmd]; ok {
		return &BigOpNode{Kind: cmd, IsVector: true, DefaultLimits: defLimits}
	}
	if defLimits, ok := namedOperators[cmd]; ok {
		return &BigOpNode{Kind: cmd, IsVector: false, DefaultLimits: defLimits}
	}
	if sym, ok := texSymbols[cmd]; ok {
		return &SymbolNode{Text: sym.glyph, Italic: sym.italic, Class: sym.class}
	}

	// Unknown command fallback: render command name upright rather than dropping content
	clean := strings.TrimFunc(cmd, unicode.IsSpace)
	if clean == "" {
		return nil
	}
	return &TextRunNode{Text: clean}
}

func (p *parser) parseEnvironment(env string) Node {
	// Optional column spec on \begin{array}{ccc}
	if env == "array" {
		p.skipSpaces()
		if !p.eof() && p.peek() == '{' {
			_ = p.readBracedRawText()
		}
	}

	var rows [][]*RowNode
	var curRow []*RowNode

	for !p.eof() {
		cell := p.parseRow(stopMatrixCellOrEnd)
		curRow = append(curRow, cell)
		p.skipSpaces()
		if p.eof() {
			break
		}
		if p.peek() == '&' {
			p.pos++
			continue
		}
		if strings.HasPrefix(p.src[p.pos:], `\\`) {
			p.pos += 2
			// Optional row spacing e.g. \\[4pt]
			p.skipSpaces()
			if !p.eof() && p.peek() == '[' {
				_ = p.readBracketOptional()
			}
			rows = append(rows, curRow)
			curRow = nil
			continue
		}
		if p.hasCommandAhead(`\end`) {
			p.skipSpaces()
			p.pos += len(`\end`)
			_ = p.readBracedRawText()
			break
		}
		// Safety progress guard
		p.pos++
	}
	if len(curRow) > 0 && (len(curRow) > 1 || len(curRow[0].Items) > 0) {
		rows = append(rows, curRow)
	}
	return &MatrixNode{Env: env, Rows: rows}
}
