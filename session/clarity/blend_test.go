package clarity

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"
)

// TestBlend1D proves lipgloss v2's Blend1D gradient primitive is really
// wired up: three Digital Clarity brand stops in, a middle colour out that
// is neither endpoint.
func TestBlend1D(t *testing.T) {
	stops := []color.Color{
		lipgloss.Color("#73F479"),
		lipgloss.Color("#54E6EA"),
		lipgloss.Color("#EAC4F2"),
	}

	steps := lipgloss.Blend1D(9, stops...)
	if len(steps) != 9 {
		t.Fatalf("Blend1D(9, ...) returned %d steps, want 9", len(steps))
	}

	middle := steps[len(steps)/2]
	mr, mg, mb, _ := middle.RGBA()

	for i, endpoint := range []color.Color{stops[0], stops[len(stops)-1]} {
		er, eg, eb, _ := endpoint.RGBA()
		if mr == er && mg == eg && mb == eb {
			t.Fatalf("middle colour %v equals endpoint %d (%v) - gradient did not blend", middle, i, endpoint)
		}
	}
}

// TestHarmonicaSpring proves the harmonica spring primitive pulled in
// alongside lipgloss v2's Blend1D/Blend2D resolves and behaves like a
// damped spring: repeated updates move the value toward its target without
// overshooting past it in the wrong direction.
func TestHarmonicaSpring(t *testing.T) {
	spring := harmonica.NewSpring(harmonica.FPS(60), 6.0, 1.0)

	pos, velocity := 0.0, 0.0
	const target = 100.0
	for i := 0; i < 120; i++ {
		pos, velocity = spring.Update(pos, velocity, target)
	}

	if pos <= 0 || pos > target+1 {
		t.Fatalf("spring did not converge toward target: pos=%v target=%v", pos, target)
	}
}
