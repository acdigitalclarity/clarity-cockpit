package clarity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLanePath_DefaultRoot(t *testing.T) {
	os.Unsetenv(SessionsRootEnvVar)

	got, err := ResolveLanePath("ways-of-working")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(DefaultSessionsRoot, "ways-of-working")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveLanePath_RootOverride(t *testing.T) {
	t.Setenv(SessionsRootEnvVar, "/tmp/fake-sessions-root")

	got, err := ResolveLanePath("some-lane")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join("/tmp/fake-sessions-root", "some-lane")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveLanePath_RejectsEmpty(t *testing.T) {
	if _, err := ResolveLanePath(""); err == nil {
		t.Error("expected error for empty lane name, got nil")
	}
	if _, err := ResolveLanePath("   "); err == nil {
		t.Error("expected error for whitespace-only lane name, got nil")
	}
}

func TestResolveLanePath_RejectsPathTraversal(t *testing.T) {
	cases := []string{
		"..",
		".",
		"../escape",
		"foo/../../etc",
		"/absolute/path",
		"nested/lane",
	}
	for _, lane := range cases {
		if _, err := ResolveLanePath(lane); err == nil {
			t.Errorf("expected error for lane %q, got nil", lane)
		}
	}
}

func TestResolveExistingLaneDir_MissingDir(t *testing.T) {
	t.Setenv(SessionsRootEnvVar, t.TempDir())

	if _, err := ResolveExistingLaneDir("does-not-exist"); err == nil {
		t.Error("expected error for a lane directory that does not exist, got nil")
	}
}

func TestResolveExistingLaneDir_RejectsFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv(SessionsRootEnvVar, root)

	filePath := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to write fixture file: %v", err)
	}

	if _, err := ResolveExistingLaneDir("not-a-dir"); err == nil {
		t.Error("expected error when the lane path is a file, got nil")
	}
}

func TestResolveExistingLaneDir_ValidDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv(SessionsRootEnvVar, root)

	laneDir := filepath.Join(root, "ways-of-working")
	if err := os.Mkdir(laneDir, 0o755); err != nil {
		t.Fatalf("failed to create fixture lane dir: %v", err)
	}

	got, err := ResolveExistingLaneDir("ways-of-working")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != laneDir {
		t.Errorf("got %q, want %q", got, laneDir)
	}
}
