package markdown

import "testing"

func TestCompileSampleMathDocument(t *testing.T) {
	doc := Compile(SampleMathDocumentMarkdown)
	if len(doc.Outline) < 9 {
		t.Fatalf("expected at least 9 outline headings, got %d: %+v", len(doc.Outline), doc.Outline)
	}

	mathBlocks := 0
	inlineMaths := 0
	for _, b := range doc.Blocks {
		if b.Kind == "math_block" && b.Math != nil && b.Math.SVG != "" {
			mathBlocks++
		}
		for _, in := range b.Inlines {
			if in.Kind == "math_inline" && in.Math != nil && in.Math.SVG != "" {
				inlineMaths++
			}
		}
	}
	if mathBlocks < 9 {
		t.Fatalf("expected >= 9 native rendered math blocks, got %d", mathBlocks)
	}
	if inlineMaths < 5 {
		t.Fatalf("expected >= 5 native rendered inline math spans, got %d", inlineMaths)
	}
}
