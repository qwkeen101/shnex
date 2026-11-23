package texmath

import (
	"strings"
	"unicode"
)

// AtomClass models Knuth's TeX math atom categories for authentic inter-atom spacing.
type AtomClass int

const (
	AtomOrd AtomClass = iota
	AtomOp
	AtomBin
	AtomRel
	AtomOpen
	AtomClose
	AtomPunct
	AtomInner
)

// MathStyle represents the 4 standard TeX math styles.
type MathStyle int

const (
	StyleDisplay MathStyle = iota
	StyleText
	StyleScript
	StyleScriptScript
)

func (s MathStyle) NextScript() MathStyle {
	switch s {
	case StyleDisplay, StyleText:
		return StyleScript
	default:
		return StyleScriptScript
	}
}

func (s MathStyle) NextFrac() MathStyle {
	switch s {
	case StyleDisplay:
		return StyleText
	case StyleText:
		return StyleScript
	default:
		return StyleScriptScript
	}
}

func (s MathStyle) Scale() float64 {
	switch s {
	case StyleDisplay, StyleText:
		return 1.0
	case StyleScript:
		return 0.72
	default:
		return 0.56
	}
}

type symbolDef struct {
	glyph  string
	italic bool
	class  AtomClass
}

var texSymbols = map[string]symbolDef{
	// Lowercase Greek (italic in TeX)
	"alpha":      {"α", true, AtomOrd},
	"beta":       {"β", true, AtomOrd},
	"gamma":      {"γ", true, AtomOrd},
	"delta":      {"δ", true, AtomOrd},
	"epsilon":    {"ϵ", true, AtomOrd},
	"varepsilon": {"ε", true, AtomOrd},
	"zeta":       {"ζ", true, AtomOrd},
	"eta":        {"η", true, AtomOrd},
	"theta":      {"θ", true, AtomOrd},
	"vartheta":   {"ϑ", true, AtomOrd},
	"iota":       {"ι", true, AtomOrd},
	"kappa":      {"κ", true, AtomOrd},
	"lambda":     {"λ", true, AtomOrd},
	"mu":         {"μ", true, AtomOrd},
	"nu":         {"ν", true, AtomOrd},
	"xi":         {"ξ", true, AtomOrd},
	"pi":         {"π", true, AtomOrd},
	"varpi":      {"ϖ", true, AtomOrd},
	"rho":        {"ρ", true, AtomOrd},
	"varrho":     {"ϱ", true, AtomOrd},
	"sigma":      {"σ", true, AtomOrd},
	"varsigma":   {"ς", true, AtomOrd},
	"tau":        {"τ", true, AtomOrd},
	"upsilon":    {"υ", true, AtomOrd},
	"phi":        {"ϕ", true, AtomOrd},
	"varphi":     {"φ", true, AtomOrd},
	"chi":        {"χ", true, AtomOrd},
	"psi":        {"ψ", true, AtomOrd},
	"omega":      {"ω", true, AtomOrd},

	// Uppercase Greek (upright in TeX)
	"Gamma":   {"Γ", false, AtomOrd},
	"Delta":   {"Δ", false, AtomOrd},
	"Theta":   {"Θ", false, AtomOrd},
	"Lambda":  {"Λ", false, AtomOrd},
	"Xi":      {"Ξ", false, AtomOrd},
	"Pi":      {"Π", false, AtomOrd},
	"Sigma":   {"Σ", false, AtomOrd},
	"Upsilon": {"Υ", false, AtomOrd},
	"Phi":     {"Φ", false, AtomOrd},
	"Psi":     {"Ψ", false, AtomOrd},
	"Omega":   {"Ω", false, AtomOrd},

	// Calculus, Logic & Misc Ord Symbols
	"partial":    {"∂", false, AtomOrd},
	"nabla":      {"∇", false, AtomOrd},
	"infty":      {"∞", false, AtomOrd},
	"hbar":       {"ℏ", true, AtomOrd},
	"ell":        {"ℓ", true, AtomOrd},
	"Re":         {"ℜ", false, AtomOrd},
	"Im":         {"ℑ", false, AtomOrd},
	"emptyset":   {"∅", false, AtomOrd},
	"varnothing": {"∅", false, AtomOrd},
	"forall":     {"∀", false, AtomOrd},
	"exists":     {"∃", false, AtomOrd},
	"nexists":    {"∄", false, AtomOrd},
	"neg":        {"¬", false, AtomOrd},
	"lnot":       {"¬", false, AtomOrd},
	"prime":      {"′", false, AtomOrd},
	"angle":      {"∠", false, AtomOrd},
	"triangle":   {"△", false, AtomOrd},
	"dots":       {"…", false, AtomInner},
	"ldots":      {"…", false, AtomInner},
	"cdots":      {"⋯", false, AtomInner},
	"vdots":      {"⋮", false, AtomOrd},
	"ddots":      {"⋱", false, AtomOrd},

	// Binary Operators
	"cdot":     {"⋅", false, AtomBin},
	"times":    {"×", false, AtomBin},
	"div":      {"÷", false, AtomBin},
	"pm":       {"±", false, AtomBin},
	"mp":       {"∓", false, AtomBin},
	"ast":      {"∗", false, AtomBin},
	"star":     {"⋆", false, AtomBin},
	"circ":     {"∘", false, AtomBin},
	"bullet":   {"∙", false, AtomBin},
	"oplus":    {"⊕", false, AtomBin},
	"ominus":   {"⊖", false, AtomBin},
	"otimes":   {"⊗", false, AtomBin},
	"cup":      {"∪", false, AtomBin},
	"cap":      {"∩", false, AtomBin},
	"setminus": {"∖", false, AtomBin},
	"wedge":    {"∧", false, AtomBin},
	"land":     {"∧", false, AtomBin},
	"vee":      {"∨", false, AtomBin},
	"lor":      {"∨", false, AtomBin},

	// Relations & Arrows
	"to":                 {"→", false, AtomRel},
	"rightarrow":         {"→", false, AtomRel},
	"leftarrow":          {"←", false, AtomRel},
	"leftrightarrow":     {"↔", false, AtomRel},
	"Rightarrow":         {"⇒", false, AtomRel},
	"Leftarrow":          {"⇐", false, AtomRel},
	"Leftrightarrow":     {"⇔", false, AtomRel},
	"iff":                {"⇔", false, AtomRel},
	"implies":            {"⇒", false, AtomRel},
	"mapsto":             {"↦", false, AtomRel},
	"longrightarrow":     {"⟶", false, AtomRel},
	"longleftarrow":      {"⟵", false, AtomRel},
	"Longrightarrow":     {"⟹", false, AtomRel},
	"le":                 {"≤", false, AtomRel},
	"leq":                {"≤", false, AtomRel},
	"ge":                 {"≥", false, AtomRel},
	"geq":                {"≥", false, AtomRel},
	"ne":                 {"≠", false, AtomRel},
	"neq":                {"≠", false, AtomRel},
	"approx":             {"≈", false, AtomRel},
	"sim":                {"∼", false, AtomRel},
	"simeq":              {"≃", false, AtomRel},
	"cong":               {"≅", false, AtomRel},
	"equiv":              {"≡", false, AtomRel},
	"propto":             {"∝", false, AtomRel},
	"ll":                 {"≪", false, AtomRel},
	"gg":                 {"≫", false, AtomRel},
	"in":                 {"∈", false, AtomRel},
	"notin":              {"∉", false, AtomRel},
	"ni":                 {"∋", false, AtomRel},
	"subset":             {"⊂", false, AtomRel},
	"supset":             {"⊃", false, AtomRel},
	"subseteq":           {"⊆", false, AtomRel},
	"supseteq":           {"⊇", false, AtomRel},
	"mid":                {"|", false, AtomRel},
	"parallel":           {"∥", false, AtomRel},
	"perp":               {"⊥", false, AtomRel},
	"models":             {"⊨", false, AtomRel},

	// Delimiters
	"langle": {"⟨", false, AtomOpen},
	"rangle": {"⟩", false, AtomClose},
	"lfloor": {"⌊", false, AtomOpen},
	"rfloor": {"⌋", false, AtomClose},
	"lceil":  {"⌈", false, AtomOpen},
	"rceil":  {"⌉", false, AtomClose},
}

// Named upright operators and whether they default to under/over limits in Display style.
var namedOperators = map[string]bool{
	"lim":    true,
	"limsup": true,
	"liminf": true,
	"max":    true,
	"min":    true,
	"sup":    true,
	"inf":    true,
	"argmax": true,
	"argmin": true,
	"det":    true,
	"gcd":    true,
	"Pr":     true,

	"sin":    false,
	"cos":    false,
	"tan":    false,
	"cot":    false,
	"sec":    false,
	"csc":    false,
	"arcsin": false,
	"arccos": false,
	"arctan": false,
	"sinh":   false,
	"cosh":   false,
	"tanh":   false,
	"coth":   false,
	"ln":     false,
	"log":    false,
	"lg":     false,
	"exp":    false,
	"dim":    false,
	"ker":    false,
	"deg":    false,
	"hom":    false,
	"arg":    false,
	"mod":    false,
}

// Vector-drawn big operators and whether they default to under/over limits in Display style.
// Per Knuth's TeXbook: \sum, \prod, \bigcup, \bigcap place limits above/below in Display style,
// while integrals (\int, \iint, \iiint, \oint) place limits to the side unless \limits is specified.
var vectorBigOps = map[string]bool{
	"sum":    true,
	"prod":   true,
	"coprod": true,
	"bigcup": true,
	"bigcap": true,
	"int":    false,
	"iint":   false,
	"iiint":  false,
	"oint":   false,
}

var spaceCommandsMu = map[string]float64{
	",":     3.0,
	":":     4.0,
	">":     4.0,
	";":     5.0,
	"!":     -3.0,
	" ":     6.0,
	"quad":  18.0,
	"qquad": 36.0,
}

func toBlackboardBold(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case 'C':
			b.WriteRune('ℂ')
		case 'H':
			b.WriteRune('ℍ')
		case 'N':
			b.WriteRune('ℕ')
		case 'P':
			b.WriteRune('ℙ')
		case 'Q':
			b.WriteRune('ℚ')
		case 'R':
			b.WriteRune('ℝ')
		case 'Z':
			b.WriteRune('ℤ')
		default:
			if r >= 'A' && r <= 'Z' {
				b.WriteRune(rune(0x1D538 + (r - 'A')))
			} else if r >= 'a' && r <= 'z' {
				b.WriteRune(rune(0x1D552 + (r - 'a')))
			} else if r >= '0' && r <= '9' {
				b.WriteRune(rune(0x1D7D8 + (r - '0')))
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// interAtomSpaceMu implements the standard TeXbook inter-atom spacing table (in mu = em/18).
func interAtomSpaceMu(left, right AtomClass, style MathStyle) float64 {
	inScript := style == StyleScript || style == StyleScriptScript
	switch left {
	case AtomOrd:
		switch right {
		case AtomOp:
			return 3
		case AtomBin:
			if inScript {
				return 0
			}
			return 4
		case AtomRel:
			if inScript {
				return 0
			}
			return 5
		case AtomInner:
			if inScript {
				return 0
			}
			return 3
		}
	case AtomOp:
		switch right {
		case AtomOrd, AtomOp:
			return 3
		case AtomRel:
			if inScript {
				return 0
			}
			return 5
		case AtomInner:
			if inScript {
				return 0
			}
			return 3
		}
	case AtomBin:
		if inScript {
			return 0
		}
		switch right {
		case AtomOrd, AtomOp, AtomOpen, AtomInner:
			return 4
		}
	case AtomRel:
		if inScript {
			return 0
		}
		switch right {
		case AtomOrd, AtomOp, AtomOpen, AtomInner:
			return 5
		}
	case AtomClose:
		switch right {
		case AtomOp:
			return 3
		case AtomBin:
			if inScript {
				return 0
			}
			return 4
		case AtomRel:
			if inScript {
				return 0
			}
			return 5
		case AtomInner:
			if inScript {
				return 0
			}
			return 3
		}
	case AtomPunct:
		if inScript {
			return 0
		}
		switch right {
		case AtomOrd, AtomOp, AtomRel, AtomOpen, AtomClose, AtomPunct, AtomInner:
			return 3
		}
	case AtomInner:
		switch right {
		case AtomOp:
			return 3
		case AtomBin:
			if inScript {
				return 0
			}
			return 4
		case AtomRel:
			if inScript {
				return 0
			}
			return 5
		case AtomOrd, AtomOpen, AtomPunct, AtomInner:
			if inScript {
				return 0
			}
			return 3
		}
	}
	return 0
}

// runeWidthFactor estimates serif mathematical advance width as a fraction of fontSize.
func runeWidthFactor(r rune, italic bool) float64 {
	if r >= 0x4E00 && r <= 0x9FFF || unicode.Is(unicode.Han, r) {
		return 1.0
	}
	switch r {
	case 'i', 'j', 'l', 'I', '!', '.', ',', ':', ';', '\'', '|', '∣':
		if italic && (r == 'i' || r == 'j' || r == 'l') {
			return 0.34
		}
		return 0.30
	case 'f', 't', 'r', '1', '(', ')', '[', ']', '{', '}':
		if italic && r == 'f' {
			return 0.44
		}
		return 0.38
	case 'm', 'w', 'M', 'W', 'Ω', 'Σ', 'Π', '∞', '⇔', '⇒', '⇐', '⟶', '⟵', '⟹':
		if r == 'm' || r == 'w' {
			return 0.78
		}
		return 0.86
	case '+', '-', '=', '<', '>', '≤', '≥', '≠', '≈', '≡', '±', '∓', '×', '÷', '∈', '∉', '⊂', '⊃', '⊆', '⊇', '→', '←', '↔', '↦', '∪', '∩', '⊕', '⊗':
		return 0.68
	case '⋅', '∙', '∘':
		return 0.34
	case ' ':
		return 0.33
	default:
		if r >= 'A' && r <= 'Z' {
			return 0.68
		}
		if r >= '0' && r <= '9' {
			return 0.52
		}
		return 0.54
	}
}
