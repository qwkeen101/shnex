package texmath

import (
	"strings"
	"testing"
)

func TestRenderAllScreenshotMathConstructs(t *testing.T) {
	formulas := []struct {
		name string
		tex  string
	}{
		{"limit_sinc", `\lim_{x \to 0} \frac{\sin x}{x} = 1`},
		{"derivatives", `\frac{\mathrm{d}}{\mathrm{d}x}\left(x^3 + 2x\right) = 3x^2 + 2, \quad \frac{\partial^2 f}{\partial x \partial y}`},
		{"integrals_and_vectors", `\int_0^1 x^2\,\mathrm{d}x = \frac{1}{3}, \quad \iint_D (x + y)\,\mathrm{d}x\,\mathrm{d}y, \quad \oint_C \vec{F} \cdot \mathrm{d}\vec{r}`},
		{"stretchy_delims", `\left(\frac{a}{b}\right), \quad \left[\sum_{i=1}^n x_i\right], \quad \{x \in \mathbb{R} \mid x > 0\}, \quad |x - 1| < \varepsilon`},
		{"matrices_top", `\begin{matrix} a & b \\ c & d \end{matrix} \quad \begin{pmatrix} 1 & 0 & 0 \\ 0 & 1 & 0 \\ 0 & 0 & 1 \end{pmatrix}`},
		{"matrices_bottom", `\begin{bmatrix} 1 & 2 & 3 \\ 4 & 5 & 6 \end{bmatrix} \quad \begin{vmatrix} a & b \\ c & d \end{vmatrix} = ad - bc`},
		{"radicals_and_binom", `\sqrt[3]{x^2 + 1} + \binom{n}{k} = \sqrt{\frac{\alpha + \beta}{\gamma}}`},
		{"piecewise_cases", `f(x) = \begin{cases} x^2, & x \ge 0 \\ -x, & x < 0 \end{cases}`},
	}

	for _, tc := range formulas {
		t.Run(tc.name, func(t *testing.T) {
			res := Render(tc.tex, true, 18)
			if !strings.HasPrefix(res.SVG, "<svg ") || !strings.HasSuffix(res.SVG, "</svg>") {
				t.Fatalf("invalid SVG output for %s", tc.name)
			}
			if res.Width <= 10 || res.Height <= 10 || res.Ascent <= 0 || res.Descent <= 0 {
				t.Fatalf("invalid metrics for %s: %+v", tc.name, res)
			}
		})
	}
}
