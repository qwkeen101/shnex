package markdown

const SampleMathDocumentName = "math-rendering.md"

const SampleMathDocumentMarkdown = `# 原生 Markdown 与数学公式排版演示

本文档由 **Shnex Go 核心引擎**（` + "`core/markdown`" + ` 与 ` + "`core/texmath`" + `）直接进行原生盒模型（Box-and-Glue）计算与纯矢量几何排版，无需外部前端公式库或子 WebView。

> [!TIP] 跨端一致性架构
> 数学公式的语法树解析、基线轴对齐（Math Axis）、分式横线、根号折线、积分贝塞尔曲线以及可伸缩括号均在 Go 核心层原生生成，桌面端与后续 Android 原生界面可直接复用同一套排版结果。

## 1. 行内公式与上下标

设质能方程为 $E = mc^2$，欧拉恒等式为 $e^{i\pi} + 1 = 0$。当序列满足 $a_{n+1} = r \cdot a_n^2 + b_{n-1}$ 时，函数导数可记作 $f'(x)$ 与 $f''(x)$。

## 2. 分式与二项式系数

$$
\frac{a + b}{c + d} + \frac{1}{1 + \frac{1}{x + 1}} = \binom{n}{k} p^k (1 - p)^{n-k}
$$

## 3. 根式与高次方根

$$
x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}, \quad \sqrt[3]{x^3 + y^3 + 1} = \sqrt{\frac{\alpha^2 + \beta^2}{\gamma}}
$$

## 4. 希腊字母与数集符号

对于任意 $\forall \varepsilon > 0$，存在 $\delta > 0$，使得当 $x \in \mathbb{R} \setminus \mathbb{Q}$ 且 $\theta \in [0, 2\pi)$ 时：

$$
\Gamma(z) = \int_0^\infty t^{z-1} e^{-t}\,\mathrm{d}t, \quad z \in \mathbb{C}
$$

## 5. 求和、连乘与集合并交

$$
\sum_{k=1}^n k^2 = \frac{n(n+1)(2n+1)}{6}, \quad \prod_{i=1}^n \left(1 + x_i\right), \quad \bigcup_{i=1}^n A_i \subseteq \bigcap_{j=1}^m B_j
$$

## 6. 向量重音与分段函数

$$
\hat{u} = \frac{\vec{v}}{\|\vec{v}\|}, \quad \overline{z_1 + z_2} = \bar{z}_1 + \bar{z}_2, \quad \mathrm{ReLU}(x) = \begin{cases} x, & x \ge 0 \\ 0, & x < 0 \end{cases}
$$

## 7. 极限、导数与积分

$$
\lim_{x \to 0} \frac{\sin x}{x} = 1
$$

$$
\frac{\mathrm{d}}{\mathrm{d}x}\left(x^3 + 2x\right) = 3x^2 + 2, \quad \frac{\partial^2 f}{\partial x \partial y}
$$

$$
\int_0^1 x^2\,\mathrm{d}x = \frac{1}{3}, \quad \iint_D (x + y)\,\mathrm{d}x\,\mathrm{d}y, \quad \oint_C \vec{F} \cdot \mathrm{d}\vec{r}
$$

## 8. 伸缩括号与绝对值

$$
\left(\frac{a}{b}\right), \quad \left[\sum_{i=1}^n x_i\right], \quad \{x \in \mathbb{R} \mid x > 0\}, \quad |x - 1| < \varepsilon
$$

## 9. 矩阵

$$
\begin{matrix} a & b \\ c & d \end{matrix} \quad \begin{pmatrix} 1 & 0 & 0 \\ 0 & 1 & 0 \\ 0 & 0 & 1 \end{pmatrix}
$$

$$
\begin{bmatrix} 1 & 2 & 3 \\ 4 & 5 & 6 \end{bmatrix} \quad \begin{vmatrix} a & b \\ c & d \end{vmatrix} = ad - bc
$$
`
