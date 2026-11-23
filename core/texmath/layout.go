package texmath

import (
	"bytes"
	"fmt"
	"html"
	"math"
	"strings"
)

// RenderedMath holds the self-contained vector SVG and exact baseline metrics of a typeset formula.
type RenderedMath struct {
	TeX     string  `json:"tex"`
	Display bool    `json:"display"`
	SVG     string  `json:"svg"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	Ascent  float64 `json:"ascent"`
	Descent float64 `json:"descent"`
}

const mathFontStack = `'Cambria Math', 'STIX Two Math', 'Times New Roman', Georgia, 'Noto Serif SC', serif`

type drawPrimitive interface {
	emitSVG(buf *bytes.Buffer, dx, dy float64)
}

type textPrim struct {
	x, y     float64
	text     string
	fontSize float64
	italic   bool
	bold     bool
}

func (t textPrim) emitSVG(buf *bytes.Buffer, dx, dy float64) {
	if t.text == "" {
		return
	}
	style := ""
	if t.italic {
		style += "font-style:italic;"
	}
	if t.bold {
		style += "font-weight:700;"
	}
	fmt.Fprintf(
		buf,
		`<text x="%.2f" y="%.2f" font-size="%.2f" font-family="%s" fill="currentColor"%s>%s</text>`,
		t.x+dx,
		t.y+dy,
		t.fontSize,
		mathFontStack,
		attrIf(style != "", ` style="`+style+`"`),
		html.EscapeString(t.text),
	)
}

type linePrim struct {
	x1, y1, x2, y2 float64
	strokeWidth    float64
}

func (l linePrim) emitSVG(buf *bytes.Buffer, dx, dy float64) {
	fmt.Fprintf(
		buf,
		`<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="currentColor" stroke-width="%.2f" stroke-linecap="round"/>`,
		l.x1+dx, l.y1+dy, l.x2+dx, l.y2+dy, l.strokeWidth,
	)
}

type pathPrim struct {
	d           string
	strokeWidth float64
	fill        bool
	dx, dy      float64
}

func (p pathPrim) emitSVG(buf *bytes.Buffer, ox, oy float64) {
	tx := ox + p.dx
	ty := oy + p.dy
	fillAttr := `fill="none"`
	if p.fill {
		fillAttr = `fill="currentColor"`
	}
	strokeAttr := ""
	if p.strokeWidth > 0 {
		strokeAttr = fmt.Sprintf(` stroke="currentColor" stroke-width="%.2f" stroke-linecap="round" stroke-linejoin="round"`, p.strokeWidth)
	}
	fmt.Fprintf(
		buf,
		`<path transform="translate(%.2f,%.2f)" d="%s" %s%s/>`,
		tx, ty, p.d, fillAttr, strokeAttr,
	)
}

type circlePrim struct {
	cx, cy      float64
	rx, ry      float64
	strokeWidth float64
	fill        bool
}

func (c circlePrim) emitSVG(buf *bytes.Buffer, dx, dy float64) {
	if c.fill {
		fmt.Fprintf(
			buf,
			`<ellipse cx="%.2f" cy="%.2f" rx="%.2f" ry="%.2f" fill="currentColor"/>`,
			c.cx+dx, c.cy+dy, c.rx, c.ry,
		)
	} else {
		fmt.Fprintf(
			buf,
			`<ellipse cx="%.2f" cy="%.2f" rx="%.2f" ry="%.2f" fill="none" stroke="currentColor" stroke-width="%.2f"/>`,
			c.cx+dx, c.cy+dy, c.rx, c.ry, c.strokeWidth,
		)
	}
}

type childBox struct {
	dx, dy float64
	box    *Box
}

// Box represents a typeset TeX horizontal/vertical box with baseline at y = 0.
type Box struct {
	Width    float64
	Ascent   float64
	Descent  float64
	Class    AtomClass
	SkewX    float64 // italic slant adjustment for BigOp limits
	prims    []drawPrimitive
	children []childBox
}

func (b *Box) emit(buf *bytes.Buffer, ox, oy float64) {
	for _, p := range b.prims {
		p.emitSVG(buf, ox, oy)
	}
	for _, ch := range b.children {
		ch.box.emit(buf, ox+ch.dx, oy+ch.dy)
	}
}

func attrIf(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

// Render typesets a TeX expression into pure vector SVG with zero external dependencies.
func Render(tex string, display bool, baseFontSize float64) RenderedMath {
	if baseFontSize <= 0 {
		if display {
			baseFontSize = 18.0
		} else {
			baseFontSize = 16.0
		}
	}
	ast := Parse(tex)
	style := StyleText
	if display {
		style = StyleDisplay
	}
	root := layoutNode(ast, style, baseFontSize)

	padX := 2.0
	padY := 2.0
	ascent := math.Max(root.Ascent+padY, baseFontSize*0.62)
	descent := math.Max(root.Descent+padY, baseFontSize*0.22)
	width := math.Max(root.Width+padX*2, 4.0)
	height := ascent + descent

	var body bytes.Buffer
	root.emit(&body, padX, ascent)

	var svg bytes.Buffer
	fmt.Fprintf(
		&svg,
		`<svg xmlns="http://www.w3.org/2000/svg" width="%.2f" height="%.2f" viewBox="0 0 %.2f %.2f" role="math" aria-label="%s" style="vertical-align:-%.2fpx;overflow:visible;display:inline-block">`,
		width,
		height,
		width,
		height,
		html.EscapeString(strings.TrimSpace(tex)),
		descent,
	)
	svg.Write(body.Bytes())
	svg.WriteString(`</svg>`)

	return RenderedMath{
		TeX:     tex,
		Display: display,
		SVG:     svg.String(),
		Width:   width,
		Height:  height,
		Ascent:  ascent,
		Descent: descent,
	}
}

func fontSizeFor(style MathStyle, base float64) float64 {
	return base * style.Scale()
}

func axisHeight(fs float64) float64 {
	return 0.27 * fs
}

func ruleThickness(fs float64) float64 {
	return math.Max(1.05, 0.058*fs)
}

func layoutNode(n Node, style MathStyle, baseFS float64) *Box {
	if n == nil {
		return &Box{}
	}
	switch v := n.(type) {
	case *RowNode:
		return layoutRow(v, style, baseFS)
	case *SymbolNode:
		return layoutSymbol(v, style, baseFS)
	case *TextRunNode:
		return layoutTextRun(v, style, baseFS)
	case *SpaceNode:
		fs := fontSizeFor(style, baseFS)
		w := (v.Mu / 18.0) * fs
		return &Box{Width: w, Class: AtomOrd}
	case *BigOpNode:
		return layoutBigOp(v, style, baseFS)
	case *ScriptsNode:
		return layoutScripts(v, style, baseFS)
	case *FracNode:
		return layoutFrac(v, style, baseFS)
	case *SqrtNode:
		return layoutSqrt(v, style, baseFS)
	case *DelimitedNode:
		return layoutDelimited(v, style, baseFS)
	case *AccentNode:
		return layoutAccent(v, style, baseFS)
	case *MatrixNode:
		return layoutMatrix(v, style, baseFS)
	default:
		return &Box{}
	}
}

func layoutRow(r *RowNode, style MathStyle, baseFS float64) *Box {
	if r == nil || len(r.Items) == 0 {
		return &Box{}
	}
	fs := fontSizeFor(style, baseFS)
	boxes := make([]*Box, 0, len(r.Items))
	classes := make([]AtomClass, 0, len(r.Items))

	for _, item := range r.Items {
		b := layoutNode(item, style, baseFS)
		boxes = append(boxes, b)
		classes = append(classes, b.Class)
	}

	// TeX unary operator conversion: a Bin at start or after Bin/Op/Rel/Open/Punct becomes Ord
	for i := range classes {
		if classes[i] == AtomBin {
			if i == 0 {
				classes[i] = AtomOrd
			} else {
				prev := classes[i-1]
				if prev == AtomBin || prev == AtomOp || prev == AtomRel || prev == AtomOpen || prev == AtomPunct {
					classes[i] = AtomOrd
				}
			}
			if i == len(classes)-1 {
				classes[i] = AtomOrd
			}
		}
	}

	out := &Box{Class: AtomOrd}
	if len(classes) == 1 {
		out.Class = classes[0]
	}

	curX := 0.0
	for i, b := range boxes {
		if i > 0 {
			mu := interAtomSpaceMu(classes[i-1], classes[i], style)
			curX += (mu / 18.0) * fs
		}
		out.children = append(out.children, childBox{dx: curX, dy: 0, box: b})
		curX += b.Width
		if b.Ascent > out.Ascent {
			out.Ascent = b.Ascent
		}
		if b.Descent > out.Descent {
			out.Descent = b.Descent
		}
	}
	out.Width = math.Max(0, curX)
	return out
}

func layoutSymbol(s *SymbolNode, style MathStyle, baseFS float64) *Box {
	fs := fontSizeFor(style, baseFS)
	w := 0.0
	for _, r := range s.Text {
		w += runeWidthFactor(r, s.Italic) * fs
	}
	if s.Italic && len(s.Text) > 0 {
		w += 0.05 * fs // slight italic optical bearing
	}
	asc := 0.74 * fs
	desc := 0.20 * fs
	if s.Text == "g" || s.Text == "j" || s.Text == "p" || s.Text == "q" || s.Text == "y" || s.Text == "μ" || s.Text == "ρ" || s.Text == "φ" || s.Text == "ϕ" || s.Text == "χ" || s.Text == "ψ" || s.Text == "(" || s.Text == ")" || s.Text == "[" || s.Text == "]" || s.Text == "{" || s.Text == "}" || s.Text == "|" {
		desc = 0.27 * fs
	}
	return &Box{
		Width:   w,
		Ascent:  asc,
		Descent: desc,
		Class:   s.Class,
		prims: []drawPrimitive{
			textPrim{
				x:        0,
				y:        0,
				text:     s.Text,
				fontSize: fs,
				italic:   s.Italic,
				bold:     s.Bold,
			},
		},
	}
}

func layoutTextRun(t *TextRunNode, style MathStyle, baseFS float64) *Box {
	fs := fontSizeFor(style, baseFS)
	w := 0.0
	for _, r := range t.Text {
		w += runeWidthFactor(r, t.Italic) * fs
	}
	return &Box{
		Width:   w,
		Ascent:  0.74 * fs,
		Descent: 0.21 * fs,
		Class:   AtomOrd,
		prims: []drawPrimitive{
			textPrim{
				x:        0,
				y:        0,
				text:     t.Text,
				fontSize: fs,
				italic:   t.Italic,
				bold:     t.Bold,
			},
		},
	}
}

func layoutBigOp(op *BigOpNode, style MathStyle, baseFS float64) *Box {
	fs := fontSizeFor(style, baseFS)
	if !op.IsVector {
		// Upright named operator like \lim, \sin, \cos, \max, \det
		w := 0.0
		for _, r := range op.Kind {
			w += runeWidthFactor(r, false) * fs
		}
		return &Box{
			Width:   w,
			Ascent:  0.72 * fs,
			Descent: 0.18 * fs,
			Class:   AtomOp,
			prims: []drawPrimitive{
				textPrim{
					x:        0,
					y:        0,
					text:     op.Kind,
					fontSize: fs,
				},
			},
		}
	}

	isDisp := style == StyleDisplay
	axis := axisHeight(fs)
	halfH := 0.72 * fs
	if isDisp {
		halfH = 1.05 * fs
	}
	topY := -axis - halfH
	botY := -axis + halfH
	totalH := botY - topY
	sw := math.Max(1.15, 0.068*fs)

	switch op.Kind {
	case "sum":
		w := totalH * 0.74
		serif := totalH * 0.12
		d := fmt.Sprintf(
			"M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f",
			w, topY+serif,
			w, topY,
			1.0, topY,
			w*0.52, -axis,
			1.0, botY,
			w, botY,
			w, botY-serif,
		)
		return &Box{
			Width:   w + 2,
			Ascent:  -topY,
			Descent: botY,
			Class:   AtomOp,
			prims:   []drawPrimitive{pathPrim{d: d, strokeWidth: sw}},
		}

	case "prod", "coprod":
		w := totalH * 0.78
		barY := topY
		if op.Kind == "coprod" {
			barY = botY
		}
		prims := []drawPrimitive{
			linePrim{x1: 0.5, y1: barY, x2: w + 1.5, y2: barY, strokeWidth: sw * 1.15},
			linePrim{x1: w*0.24 + 1, y1: topY, x2: w*0.24 + 1, y2: botY, strokeWidth: sw * 1.1},
			linePrim{x1: w*0.76 + 1, y1: topY, x2: w*0.76 + 1, y2: botY, strokeWidth: sw * 1.1},
		}
		return &Box{
			Width:   w + 2,
			Ascent:  -topY,
			Descent: botY,
			Class:   AtomOp,
			prims:   prims,
		}

	case "bigcup", "bigcap":
		w := totalH * 0.68
		var d string
		if op.Kind == "bigcup" {
			r := w * 0.46
			d = fmt.Sprintf(
				"M %.2f %.2f L %.2f %.2f A %.2f %.2f 0 0 0 %.2f %.2f L %.2f %.2f",
				1.5, topY+1,
				1.5, botY-r,
				r, r, w-0.5, botY-r,
				w-0.5, topY+1,
			)
		} else {
			r := w * 0.46
			d = fmt.Sprintf(
				"M %.2f %.2f L %.2f %.2f A %.2f %.2f 0 0 1 %.2f %.2f L %.2f %.2f",
				1.5, botY-1,
				1.5, topY+r,
				r, r, w-0.5, topY+r,
				w-0.5, botY-1,
			)
		}
		return &Box{
			Width:   w + 2,
			Ascent:  -topY,
			Descent: botY,
			Class:   AtomOp,
			prims:   []drawPrimitive{pathPrim{d: d, strokeWidth: sw * 1.05}},
		}

	default:
		// Integrals: int, iint, iiint, oint
		if isDisp {
			halfH = 1.16 * fs
			topY = -axis - halfH
			botY = -axis + halfH
			totalH = botY - topY
		}
		count := 1
		if op.Kind == "iint" {
			count = 2
		} else if op.Kind == "iiint" {
			count = 3
		}
		singleW := totalH * 0.34
		stepX := fs * 0.34
		var prims []drawPrimitive
		for k := 0; k < count; k++ {
			ox := float64(k) * stepX
			// Graceful slanted cubic Bezier S-curve integral
			d := fmt.Sprintf(
				"M %.2f %.2f C %.2f %.2f, %.2f %.2f, %.2f %.2f C %.2f %.2f, %.2f %.2f, %.2f %.2f",
				ox+singleW*0.96, topY+totalH*0.07,
				ox+singleW*0.86, topY-totalH*0.03,
				ox+singleW*0.58, topY+totalH*0.02,
				ox+singleW*0.50, -axis,
				ox+singleW*0.42, botY-totalH*0.02,
				ox+singleW*0.14, botY+totalH*0.03,
				ox+singleW*0.04, botY-totalH*0.07,
			)
			prims = append(prims,
				pathPrim{d: d, strokeWidth: sw * 1.12},
				circlePrim{cx: ox + singleW*0.92, cy: topY + totalH*0.07, rx: sw * 0.72, ry: sw * 0.72, fill: true},
				circlePrim{cx: ox + singleW*0.08, cy: botY - totalH*0.07, rx: sw * 0.72, ry: sw * 0.72, fill: true},
			)
		}
		totalW := singleW + float64(count-1)*stepX + 2
		if op.Kind == "oint" {
			prims = append(prims, circlePrim{
				cx:          singleW * 0.50,
				cy:          -axis,
				rx:          singleW * 0.34,
				ry:          singleW * 0.34,
				strokeWidth: sw * 0.92,
			})
		}
		return &Box{
			Width:   totalW,
			Ascent:  -topY,
			Descent: botY,
			Class:   AtomOp,
			SkewX:   singleW * 0.25,
			prims:   prims,
		}
	}
}

func layoutScripts(s *ScriptsNode, style MathStyle, baseFS float64) *Box {
	baseBox := layoutNode(s.Base, style, baseFS)
	scriptStyle := style.NextScript()
	fs := fontSizeFor(style, baseFS)

	var subBox, supBox *Box
	if s.Sub != nil {
		subBox = layoutNode(s.Sub, scriptStyle, baseFS)
	}
	if s.Sup != nil {
		supBox = layoutNode(s.Sup, scriptStyle, baseFS)
	}

	useLimits := false
	if op, ok := s.Base.(*BigOpNode); ok && style == StyleDisplay {
		useLimits = op.DefaultLimits
	}
	if s.ForceLimits != nil {
		useLimits = *s.ForceLimits
	}

	if useLimits {
		// Under/Over centered limits (e.g. \lim_{x\to 0}, \sum_{i=1}^n)
		maxW := baseBox.Width
		if subBox != nil && subBox.Width > maxW {
			maxW = subBox.Width
		}
		if supBox != nil && supBox.Width > maxW {
			maxW = supBox.Width
		}
		out := &Box{
			Width:   maxW,
			Ascent:  baseBox.Ascent,
			Descent: baseBox.Descent,
			Class:   baseBox.Class,
		}
		baseX := (maxW - baseBox.Width) / 2
		out.children = append(out.children, childBox{dx: baseX, dy: 0, box: baseBox})

		gap := 0.14 * fs
		if supBox != nil {
			// Place supBox bottom above -baseBox.Ascent - gap
			supY := -baseBox.Ascent - gap - supBox.Descent
			supX := (maxW - supBox.Width) / 2
			out.children = append(out.children, childBox{dx: supX, dy: supY, box: supBox})
			if -supY+supBox.Ascent > out.Ascent {
				out.Ascent = -supY + supBox.Ascent
			}
		}
		if subBox != nil {
			// Place subBox top below +baseBox.Descent + gap
			subY := baseBox.Descent + gap + subBox.Ascent
			subX := (maxW - subBox.Width) / 2
			out.children = append(out.children, childBox{dx: subX, dy: subY, box: subBox})
			if subY+subBox.Descent > out.Descent {
				out.Descent = subY + subBox.Descent
			}
		}
		return out
	}

	// Side scripts (standard superscript/subscript and integral bounds \int_0^1)
	out := &Box{
		Ascent:  baseBox.Ascent,
		Descent: baseBox.Descent,
		Class:   baseBox.Class,
	}
	out.children = append(out.children, childBox{dx: 0, dy: 0, box: baseBox})

	scriptX := baseBox.Width + 0.04*fs
	supX := scriptX
	subX := scriptX
	if baseBox.SkewX > 0 {
		// Tuck integral lower limit slightly left under the bottom curl
		subX = math.Max(baseBox.Width*0.62, scriptX-baseBox.SkewX*1.15)
		supX = scriptX + baseBox.SkewX*0.18
	}

	supShift := math.Max(0.38*fs, baseBox.Ascent-0.30*fs)
	subShift := math.Max(0.24*fs, baseBox.Descent-0.12*fs)

	if supBox != nil && subBox != nil {
		// Prevent collision between superscript bottom and subscript top
		supBottom := -supShift + supBox.Descent
		subTop := subShift - subBox.Ascent
		minClearance := 0.18 * fs
		if subTop-supBottom < minClearance {
			extra := (minClearance - (subTop - supBottom)) / 2
			supShift += extra
			subShift += extra
		}
	}

	rightEdge := baseBox.Width
	if supBox != nil {
		out.children = append(out.children, childBox{dx: supX, dy: -supShift, box: supBox})
		if supShift+supBox.Ascent > out.Ascent {
			out.Ascent = supShift + supBox.Ascent
		}
		if supX+supBox.Width > rightEdge {
			rightEdge = supX + supBox.Width
		}
	}
	if subBox != nil {
		out.children = append(out.children, childBox{dx: subX, dy: subShift, box: subBox})
		if subShift+subBox.Descent > out.Descent {
			out.Descent = subShift + subBox.Descent
		}
		if subX+subBox.Width > rightEdge {
			rightEdge = subX + subBox.Width
		}
	}
	out.Width = rightEdge + 0.05*fs
	return out
}

func layoutFrac(f *FracNode, style MathStyle, baseFS float64) *Box {
	innerStyle := style.NextFrac()
	if f.ForceStyle != nil {
		innerStyle = f.ForceStyle.NextFrac()
	}
	fs := fontSizeFor(style, baseFS)
	axis := axisHeight(fs)
	rt := ruleThickness(fs)

	numBox := layoutNode(f.Num, innerStyle, baseFS)
	denBox := layoutNode(f.Den, innerStyle, baseFS)

	padX := 0.18 * fs
	barW := math.Max(numBox.Width, denBox.Width) + padX*2
	gap := 0.16 * fs
	if style == StyleDisplay {
		gap = 0.22 * fs
	}

	numBaselineY := -axis - rt/2 - gap - numBox.Descent
	denBaselineY := -axis + rt/2 + gap + denBox.Ascent

	core := &Box{
		Width:   barW,
		Ascent:  -numBaselineY + numBox.Ascent,
		Descent: denBaselineY + denBox.Descent,
		Class:   AtomInner,
		children: []childBox{
			{dx: (barW - numBox.Width) / 2, dy: numBaselineY, box: numBox},
			{dx: (barW - denBox.Width) / 2, dy: denBaselineY, box: denBox},
		},
	}
	if f.HasBar {
		core.prims = append(core.prims, linePrim{
			x1:          0,
			y1:          -axis,
			x2:          barW,
			y2:          -axis,
			strokeWidth: rt,
		})
	}
	if f.WrapParen {
		return wrapWithDelimiters("(", ")", core, fs)
	}
	return core
}

func layoutSqrt(s *SqrtNode, style MathStyle, baseFS float64) *Box {
	fs := fontSizeFor(style, baseFS)
	rt := ruleThickness(fs)
	bodyBox := layoutNode(s.Body, style, baseFS)

	gapTop := 0.14*fs + rt
	padRight := 0.14 * fs
	topY := -(bodyBox.Ascent + gapTop)
	botY := bodyBox.Descent + 0.06*fs
	totalH := botY - topY

	surdW := math.Max(0.52*fs, 0.34*totalH)
	leadX := 0.0
	var children []childBox

	if s.Degree != nil {
		degBox := layoutNode(s.Degree, StyleScriptScript, baseFS)
		if degBox.Width > surdW*0.55 {
			leadX = degBox.Width - surdW*0.55
		}
		degY := topY + totalH*0.42
		children = append(children, childBox{dx: leadX + 0.04*fs, dy: degY, box: degBox})
	}

	// Surd path + horizontal vinculum overbar
	x0 := leadX
	d := fmt.Sprintf(
		"M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f",
		x0+surdW*0.08, topY+totalH*0.58,
		x0+surdW*0.26, topY+totalH*0.50,
		x0+surdW*0.54, botY,
		x0+surdW, topY,
		x0+surdW+bodyBox.Width+padRight, topY,
	)

	children = append(children, childBox{dx: x0 + surdW, dy: 0, box: bodyBox})
	totalAscent := -topY + rt
	if len(children) > 1 {
		// Ensure degree ascent is included
		degChild := children[0]
		if -degChild.dy+degChild.box.Ascent > totalAscent {
			totalAscent = -degChild.dy + degChild.box.Ascent
		}
	}

	return &Box{
		Width:    x0 + surdW + bodyBox.Width + padRight,
		Ascent:   totalAscent,
		Descent:  botY,
		Class:    AtomOrd,
		prims:    []drawPrimitive{pathPrim{d: d, strokeWidth: rt * 1.05}},
		children: children,
	}
}

func layoutDelimited(d *DelimitedNode, style MathStyle, baseFS float64) *Box {
	fs := fontSizeFor(style, baseFS)
	axis := axisHeight(fs)

	partBoxes := make([]*Box, 0, len(d.Parts))
	maxAsc := 0.55 * fs
	maxDesc := 0.18 * fs
	for _, pt := range d.Parts {
		pb := layoutRow(pt, style, baseFS)
		partBoxes = append(partBoxes, pb)
		if pb.Ascent > maxAsc {
			maxAsc = pb.Ascent
		}
		if pb.Descent > maxDesc {
			maxDesc = pb.Descent
		}
	}

	delta := math.Max(maxAsc-axis, maxDesc+axis)
	halfH := math.Max(delta*0.98+1.5, 0.54*fs)
	topY := -axis - halfH
	botY := -axis + halfH

	out := &Box{
		Ascent:  -topY,
		Descent: botY,
		Class:   AtomInner,
	}

	curX := 0.0
	if d.Left != "." && d.Left != "" {
		lb := buildStretchyDelimiterBox(d.Left, topY, botY, fs)
		out.children = append(out.children, childBox{dx: curX, dy: 0, box: lb})
		curX += lb.Width + 0.06*fs
	}

	for i, pb := range partBoxes {
		out.children = append(out.children, childBox{dx: curX, dy: 0, box: pb})
		curX += pb.Width
		if i < len(d.Mids) {
			curX += 0.16 * fs
			mb := buildStretchyDelimiterBox(d.Mids[i], topY, botY, fs)
			out.children = append(out.children, childBox{dx: curX, dy: 0, box: mb})
			curX += mb.Width + 0.16*fs
		}
	}

	if d.Right != "." && d.Right != "" {
		curX += 0.06 * fs
		rb := buildStretchyDelimiterBox(d.Right, topY, botY, fs)
		out.children = append(out.children, childBox{dx: curX, dy: 0, box: rb})
		curX += rb.Width
	}

	out.Width = curX
	return out
}

func wrapWithDelimiters(left, right string, inner *Box, fs float64) *Box {
	axis := axisHeight(fs)
	delta := math.Max(inner.Ascent-axis, inner.Descent+axis)
	halfH := math.Max(delta*0.98+1.5, 0.54*fs)
	topY := -axis - halfH
	botY := -axis + halfH

	out := &Box{
		Ascent:  math.Max(-topY, inner.Ascent),
		Descent: math.Max(botY, inner.Descent),
		Class:   AtomInner,
	}
	curX := 0.0
	if left != "" && left != "." {
		lb := buildStretchyDelimiterBox(left, topY, botY, fs)
		out.children = append(out.children, childBox{dx: curX, dy: 0, box: lb})
		curX += lb.Width + 0.08*fs
	}
	out.children = append(out.children, childBox{dx: curX, dy: 0, box: inner})
	curX += inner.Width
	if right != "" && right != "." {
		curX += 0.08 * fs
		rb := buildStretchyDelimiterBox(right, topY, botY, fs)
		out.children = append(out.children, childBox{dx: curX, dy: 0, box: rb})
		curX += rb.Width
	}
	out.Width = curX
	return out
}

func buildStretchyDelimiterBox(delim string, topY, botY, fs float64) *Box {
	totalH := botY - topY
	midY := (topY + botY) / 2
	sw := math.Max(1.10, 0.064*fs)

	switch delim {
	case "(":
		w := math.Min(0.68*fs, math.Max(0.32*fs, 0.22*fs+0.075*totalH))
		d := fmt.Sprintf(
			"M %.2f %.2f C %.2f %.2f, %.2f %.2f, %.2f %.2f",
			w-1.0, topY+1.0,
			0.5, topY+totalH*0.22,
			0.5, botY-totalH*0.22,
			w-1.0, botY-1.0,
		)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw * 1.08}}}

	case ")":
		w := math.Min(0.68*fs, math.Max(0.32*fs, 0.22*fs+0.075*totalH))
		d := fmt.Sprintf(
			"M %.2f %.2f C %.2f %.2f, %.2f %.2f, %.2f %.2f",
			1.0, topY+1.0,
			w-0.5, topY+totalH*0.22,
			w-0.5, botY-totalH*0.22,
			1.0, botY-1.0,
		)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw * 1.08}}}

	case "[":
		w := math.Min(0.52*fs, math.Max(0.28*fs, 0.20*fs+0.045*totalH))
		d := fmt.Sprintf(
			"M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f",
			w-0.5, topY+0.5,
			1.2, topY+0.5,
			1.2, botY-0.5,
			w-0.5, botY-0.5,
		)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw}}}

	case "]":
		w := math.Min(0.52*fs, math.Max(0.28*fs, 0.20*fs+0.045*totalH))
		d := fmt.Sprintf(
			"M %.2f %.2f L %.2f %.2f L %.2f %.2f L %.2f %.2f",
			0.5, topY+0.5,
			w-1.2, topY+0.5,
			w-1.2, botY-0.5,
			0.5, botY-0.5,
		)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw}}}

	case "{":
		w := math.Min(0.66*fs, math.Max(0.38*fs, 0.26*fs+0.055*totalH))
		stemX := w * 0.52
		hook := math.Min(totalH*0.14, 0.28*fs)
		d := fmt.Sprintf(
			"M %.2f %.2f Q %.2f %.2f %.2f %.2f L %.2f %.2f Q %.2f %.2f %.2f %.2f Q %.2f %.2f %.2f %.2f L %.2f %.2f Q %.2f %.2f %.2f %.2f",
			w-0.8, topY+0.5,
			stemX, topY+0.5, stemX, topY+hook,
			stemX, midY-hook,
			stemX, midY, 1.0, midY,
			stemX, midY, stemX, midY+hook,
			stemX, botY-hook,
			stemX, botY-0.5, w-0.8, botY-0.5,
		)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw}}}

	case "}":
		w := math.Min(0.66*fs, math.Max(0.38*fs, 0.26*fs+0.055*totalH))
		stemX := w * 0.48
		hook := math.Min(totalH*0.14, 0.28*fs)
		d := fmt.Sprintf(
			"M %.2f %.2f Q %.2f %.2f %.2f %.2f L %.2f %.2f Q %.2f %.2f %.2f %.2f Q %.2f %.2f %.2f %.2f L %.2f %.2f Q %.2f %.2f %.2f %.2f",
			0.8, topY+0.5,
			stemX, topY+0.5, stemX, topY+hook,
			stemX, midY-hook,
			stemX, midY, w-1.0, midY,
			stemX, midY, stemX, midY+hook,
			stemX, botY-hook,
			stemX, botY-0.5, 0.8, botY-0.5,
		)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw}}}

	case "|":
		w := 0.24 * fs
		return &Box{
			Width:   w,
			Ascent:  -topY,
			Descent: botY,
			prims:   []drawPrimitive{linePrim{x1: w / 2, y1: topY + 0.5, x2: w / 2, y2: botY - 0.5, strokeWidth: sw}},
		}

	case "‖":
		w := 0.40 * fs
		return &Box{
			Width:   w,
			Ascent:  -topY,
			Descent: botY,
			prims: []drawPrimitive{
				linePrim{x1: w * 0.28, y1: topY + 0.5, x2: w * 0.28, y2: botY - 0.5, strokeWidth: sw},
				linePrim{x1: w * 0.72, y1: topY + 0.5, x2: w * 0.72, y2: botY - 0.5, strokeWidth: sw},
			},
		}

	case "⟨":
		w := math.Min(0.56*fs, math.Max(0.32*fs, 0.16*totalH))
		d := fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f", w-1.0, topY+1.0, 1.0, midY, w-1.0, botY-1.0)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw}}}

	case "⟩":
		w := math.Min(0.56*fs, math.Max(0.32*fs, 0.16*totalH))
		d := fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f", 1.0, topY+1.0, w-1.0, midY, 1.0, botY-1.0)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw}}}

	case "⌊":
		w := 0.44 * fs
		d := fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f", 1.2, topY+0.5, 1.2, botY-0.5, w-0.5, botY-0.5)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw}}}

	case "⌋":
		w := 0.44 * fs
		d := fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f", w-1.2, topY+0.5, w-1.2, botY-0.5, 0.5, botY-0.5)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw}}}

	case "⌈":
		w := 0.44 * fs
		d := fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f", w-0.5, topY+0.5, 1.2, topY+0.5, 1.2, botY-0.5)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw}}}

	case "⌉":
		w := 0.44 * fs
		d := fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f", 0.5, topY+0.5, w-1.2, topY+0.5, w-1.2, botY-0.5)
		return &Box{Width: w, Ascent: -topY, Descent: botY, prims: []drawPrimitive{pathPrim{d: d, strokeWidth: sw}}}

	default:
		return &Box{Width: 0.2 * fs, Ascent: -topY, Descent: botY}
	}
}

func layoutAccent(a *AccentNode, style MathStyle, baseFS float64) *Box {
	fs := fontSizeFor(style, baseFS)
	sw := math.Max(1.0, 0.058*fs)
	bodyBox := layoutNode(a.Body, style, baseFS)

	w := math.Max(bodyBox.Width, 0.45*fs)
	bodyX := (w - bodyBox.Width) / 2
	topBase := -bodyBox.Ascent - 0.10*fs

	out := &Box{
		Width:    w,
		Ascent:   bodyBox.Ascent + 0.28*fs,
		Descent:  bodyBox.Descent,
		Class:    bodyBox.Class,
		children: []childBox{{dx: bodyX, dy: 0, box: bodyBox}},
	}

	switch a.Kind {
	case "vec", "overrightarrow":
		arrowW := math.Max(w*0.86, 0.48*fs)
		if a.Kind == "overrightarrow" {
			arrowW = w
		}
		x1 := (w - arrowW) / 2
		x2 := x1 + arrowW
		ay := topBase - 0.04*fs
		head := math.Min(0.18*fs, arrowW*0.36)
		d := fmt.Sprintf(
			"M %.2f %.2f L %.2f %.2f M %.2f %.2f L %.2f %.2f L %.2f %.2f",
			x1, ay, x2, ay,
			x2-head, ay-head*0.72, x2, ay, x2-head, ay+head*0.72,
		)
		out.prims = append(out.prims, pathPrim{d: d, strokeWidth: sw})

	case "bar", "overline":
		barW := w * 0.85
		if a.Kind == "overline" {
			barW = w
		}
		x1 := (w - barW) / 2
		out.prims = append(out.prims, linePrim{x1: x1, y1: topBase, x2: x1 + barW, y2: topBase, strokeWidth: sw})

	case "underline":
		uy := bodyBox.Descent + 0.12*fs
		out.Descent = uy + sw
		out.Ascent = bodyBox.Ascent
		out.prims = append(out.prims, linePrim{x1: 0, y1: uy, x2: w, y2: uy, strokeWidth: sw})

	case "hat":
		hw := math.Max(w*0.82, 0.44*fs)
		x1 := (w - hw) / 2
		mid := w / 2
		d := fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f", x1, topBase+0.05*fs, mid, topBase-0.14*fs, x1+hw, topBase+0.05*fs)
		out.prims = append(out.prims, pathPrim{d: d, strokeWidth: sw})

	case "tilde":
		tw := math.Max(w*0.84, 0.46*fs)
		x1 := (w - tw) / 2
		d := fmt.Sprintf(
			"M %.2f %.2f C %.2f %.2f, %.2f %.2f, %.2f %.2f",
			x1, topBase,
			x1+tw*0.33, topBase-0.16*fs,
			x1+tw*0.66, topBase+0.14*fs,
			x1+tw, topBase-0.02*fs,
		)
		out.prims = append(out.prims, pathPrim{d: d, strokeWidth: sw})

	case "dot":
		r := math.Max(1.15, 0.065*fs)
		out.prims = append(out.prims, circlePrim{cx: w / 2, cy: topBase - r*0.4, rx: r, ry: r, fill: true})

	case "ddot":
		r := math.Max(1.10, 0.062*fs)
		sep := 0.13 * fs
		out.prims = append(out.prims,
			circlePrim{cx: w/2 - sep, cy: topBase - r*0.4, rx: r, ry: r, fill: true},
			circlePrim{cx: w/2 + sep, cy: topBase - r*0.4, rx: r, ry: r, fill: true},
		)
	}
	return out
}

func layoutMatrix(m *MatrixNode, style MathStyle, baseFS float64) *Box {
	fs := fontSizeFor(style, baseFS)
	axis := axisHeight(fs)
	if len(m.Rows) == 0 {
		return &Box{}
	}

	cellStyle := StyleText
	if m.Env == "aligned" || m.Env == "align" || m.Env == "align*" {
		cellStyle = style
	}

	numRows := len(m.Rows)
	numCols := 0
	for _, r := range m.Rows {
		if len(r) > numCols {
			numCols = len(r)
		}
	}
	if numCols == 0 {
		return &Box{}
	}

	grid := make([][]*Box, numRows)
	colW := make([]float64, numCols)
	rowAsc := make([]float64, numRows)
	rowDesc := make([]float64, numRows)

	for ri, r := range m.Rows {
		grid[ri] = make([]*Box, numCols)
		rowAsc[ri] = 0.62 * fs
		rowDesc[ri] = 0.20 * fs
		for ci := 0; ci < numCols; ci++ {
			var cb *Box
			if ci < len(r) {
				cb = layoutRow(r[ci], cellStyle, baseFS)
			} else {
				cb = &Box{}
			}
			grid[ri][ci] = cb
			if cb.Width > colW[ci] {
				colW[ci] = cb.Width
			}
			if cb.Ascent > rowAsc[ri] {
				rowAsc[ri] = cb.Ascent
			}
			if cb.Descent > rowDesc[ri] {
				rowDesc[ri] = cb.Descent
			}
		}
	}

	colGap := 0.68 * fs
	rowGap := 0.30 * fs
	isAlign := m.Env == "aligned" || m.Env == "align" || m.Env == "align*"
	isCases := m.Env == "cases"

	totalGridH := 0.0
	for ri := 0; ri < numRows; ri++ {
		totalGridH += rowAsc[ri] + rowDesc[ri]
		if ri > 0 {
			totalGridH += rowGap
		}
	}

	totalGridW := 0.0
	colX := make([]float64, numCols)
	for ci := 0; ci < numCols; ci++ {
		if ci > 0 {
			if isAlign && ci%2 == 1 {
				totalGridW += 0.08 * fs
			} else {
				totalGridW += colGap
			}
		}
		colX[ci] = totalGridW
		totalGridW += colW[ci]
	}

	// Center grid symmetrically around -axis
	topY := -axis - totalGridH/2
	core := &Box{
		Width:   totalGridW,
		Ascent:  -topY,
		Descent: topY + totalGridH,
		Class:   AtomInner,
	}

	curY := topY
	for ri := 0; ri < numRows; ri++ {
		baselineY := curY + rowAsc[ri]
		for ci := 0; ci < numCols; ci++ {
			cb := grid[ri][ci]
			cx := colX[ci]
			if isCases {
				// Left-align in cases
			} else if isAlign {
				if ci%2 == 0 {
					// Right-align LHS before =
					cx += colW[ci] - cb.Width
				}
			} else {
				// Center-align matrix cells
				cx += (colW[ci] - cb.Width) / 2
			}
			core.children = append(core.children, childBox{dx: cx, dy: baselineY, box: cb})
		}
		curY += rowAsc[ri] + rowDesc[ri] + rowGap
	}

	switch m.Env {
	case "pmatrix":
		return wrapWithDelimiters("(", ")", core, fs)
	case "bmatrix":
		return wrapWithDelimiters("[", "]", core, fs)
	case "Bmatrix":
		return wrapWithDelimiters("{", "}", core, fs)
	case "vmatrix":
		return wrapWithDelimiters("|", "|", core, fs)
	case "Vmatrix":
		return wrapWithDelimiters("‖", "‖", core, fs)
	case "cases":
		return wrapWithDelimiters("{", ".", core, fs)
	default:
		return core
	}
}
