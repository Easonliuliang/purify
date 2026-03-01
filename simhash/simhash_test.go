package simhash

import (
	"testing"
)

func TestFingerprint_IdenticalTexts(t *testing.T) {
	text := "the quick brown fox jumps over the lazy dog"
	fp1 := Fingerprint(text)
	fp2 := Fingerprint(text)

	if fp1 != fp2 {
		t.Errorf("identical texts produced different fingerprints: %064b vs %064b", fp1, fp2)
	}
}

func TestFingerprint_SimilarTexts(t *testing.T) {
	text1 := "the quick brown fox jumps over the lazy dog"
	text2 := "the quick brown fox leaps over the lazy dog"

	fp1 := Fingerprint(text1)
	fp2 := Fingerprint(text2)

	dist := Distance(fp1, fp2)
	if dist > 10 {
		t.Errorf("similar texts have too large distance: %d (fingerprints: %064b, %064b)", dist, fp1, fp2)
	}
}

func TestFingerprint_DifferentTexts(t *testing.T) {
	text1 := "the quick brown fox jumps over the lazy dog"
	text2 := "completely unrelated content about quantum physics and mathematics"

	fp1 := Fingerprint(text1)
	fp2 := Fingerprint(text2)

	dist := Distance(fp1, fp2)
	if dist < 5 {
		t.Errorf("very different texts have too small distance: %d", dist)
	}
}

func TestFingerprint_EmptyInput(t *testing.T) {
	fp := Fingerprint("")
	if fp != 0 {
		t.Errorf("empty input should produce fingerprint 0, got: %064b", fp)
	}
}

func TestFingerprint_SingleWord(t *testing.T) {
	fp := Fingerprint("hello")
	if fp == 0 {
		t.Error("single word should produce a non-zero fingerprint")
	}

	// Same single word should be deterministic.
	fp2 := Fingerprint("hello")
	if fp != fp2 {
		t.Errorf("same single word produced different fingerprints: %d vs %d", fp, fp2)
	}
}

func TestFingerprint_WhitespaceOnly(t *testing.T) {
	fp := Fingerprint("   \t\n  ")
	if fp != 0 {
		t.Errorf("whitespace-only input should produce fingerprint 0, got: %064b", fp)
	}
}

func TestDistance(t *testing.T) {
	tests := []struct {
		name string
		a, b uint64
		want int
	}{
		{"identical", 0xFF, 0xFF, 0},
		{"all different", 0, ^uint64(0), 64},
		{"one bit", 0, 1, 1},
		{"two bits", 0, 3, 2},
		{"zero zero", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Distance(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("Distance(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSimilar(t *testing.T) {
	fp1 := Fingerprint("the quick brown fox")
	fp2 := Fingerprint("the quick brown fox")

	if !Similar(fp1, fp2, 0) {
		t.Error("identical fingerprints should be similar at threshold 0")
	}

	fp3 := Fingerprint("a completely different text about nothing related")
	dist := Distance(fp1, fp3)

	if Similar(fp1, fp3, dist-1) {
		t.Errorf("different texts should not be similar at threshold %d (distance is %d)", dist-1, dist)
	}
	if !Similar(fp1, fp3, dist) {
		t.Errorf("should be similar at threshold equal to distance (%d)", dist)
	}
}

func TestFingerprintDOM_SimilarStructures(t *testing.T) {
	html1 := `<html><head><title>Page 1</title></head><body><div><h1>Hello</h1><p>World</p></div></body></html>`
	html2 := `<html><head><title>Page 2</title></head><body><div><h1>Hi</h1><p>Earth</p></div></body></html>`

	fp1 := FingerprintDOM(html1)
	fp2 := FingerprintDOM(html2)

	if fp1 != fp2 {
		dist := Distance(fp1, fp2)
		t.Errorf("identical DOM structures should produce same fingerprint, distance: %d", dist)
	}
}

func TestFingerprintDOM_DifferentStructures(t *testing.T) {
	html1 := `<html><body><div><h1>Title</h1><p>Text</p><p>More text</p></div></body></html>`
	html2 := `<html><body><table><tr><td>A</td><td>B</td></tr><tr><td>C</td><td>D</td></tr></table></body></html>`

	fp1 := FingerprintDOM(html1)
	fp2 := FingerprintDOM(html2)

	dist := Distance(fp1, fp2)
	if dist < 3 {
		t.Errorf("different DOM structures should have larger distance, got: %d", dist)
	}
}

func TestFingerprintDOM_EmptyHTML(t *testing.T) {
	fp := FingerprintDOM("")
	if fp != 0 {
		t.Errorf("empty HTML should produce fingerprint 0, got: %064b", fp)
	}
}

func TestFingerprintDOM_PlainText(t *testing.T) {
	fp := FingerprintDOM("just some plain text with no tags")
	if fp != 0 {
		t.Errorf("plain text with no tags should produce fingerprint 0, got: %064b", fp)
	}
}

func TestFingerprintDOM_SingleTag(t *testing.T) {
	fp := FingerprintDOM("<br/>")
	if fp == 0 {
		t.Error("single self-closing tag should produce non-zero fingerprint")
	}
}

func TestFingerprintDOM_NestedStructure(t *testing.T) {
	html1 := `<div><div><div><p>Deep</p></div></div></div>`
	html2 := `<div><p>Shallow</p></div>`

	fp1 := FingerprintDOM(html1)
	fp2 := FingerprintDOM(html2)

	if fp1 == fp2 {
		t.Error("different nesting depths should produce different fingerprints")
	}
}

func TestExtractTags(t *testing.T) {
	htmlStr := `<html><head><title>Test</title></head><body><div><p>Hello</p></div></body></html>`
	tags := extractTags(htmlStr)

	expected := []string{"html", "head", "title", "body", "div", "p"}
	if len(tags) != len(expected) {
		t.Fatalf("expected %d tags, got %d: %v", len(expected), len(tags), tags)
	}

	for i, tag := range tags {
		if tag != expected[i] {
			t.Errorf("tag[%d] = %q, want %q", i, tag, expected[i])
		}
	}
}

func TestMakeShingles(t *testing.T) {
	tokens := []string{"a", "b", "c", "d"}

	shingles := makeShingles(tokens, 3)
	expected := []string{"a_b_c", "b_c_d"}

	if len(shingles) != len(expected) {
		t.Fatalf("expected %d shingles, got %d: %v", len(expected), len(shingles), shingles)
	}

	for i, s := range shingles {
		if s != expected[i] {
			t.Errorf("shingle[%d] = %q, want %q", i, s, expected[i])
		}
	}
}

func TestMakeShingles_TooFewTokens(t *testing.T) {
	shingles := makeShingles([]string{"a", "b"}, 3)
	if shingles != nil {
		t.Errorf("expected nil for fewer tokens than n, got: %v", shingles)
	}
}
