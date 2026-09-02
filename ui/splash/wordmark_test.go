package splash

import (
	"strings"
	"testing"
)

func TestWordmark_RendersSevenRowsAtGlyphWidth(t *testing.T) {
	for _, word := range []string{"CLARITY", "WORKSPACE"} {
		got := strings.Split(Wordmark(word), "\n")
		if len(got) != 7 {
			t.Fatalf("%s: want 7 rows, got %d", word, len(got))
		}
		want := glyphTextWidth(word, bigGlyphs, 1)
		for i, row := range got {
			if n := len([]rune(row)); n != want {
				t.Fatalf("%s row %d: want width %d, got %d", word, i, want, n)
			}
			if !strings.Contains(row, "█") && i != 0 {
				t.Fatalf("%s row %d has no ink", word, i)
			}
		}
	}
}

func TestWordmark_UnknownRuneIsBlank(t *testing.T) {
	rows := strings.Split(Wordmark("?"), "\n")
	for _, row := range rows {
		if strings.TrimSpace(row) != "" {
			t.Fatalf("unknown rune should render blank, got %q", row)
		}
	}
}
