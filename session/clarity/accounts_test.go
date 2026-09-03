package clarity

import (
	"os"
	"path/filepath"
	"testing"

	"claude-squad/log"

	"github.com/stretchr/testify/require"
)

func writeRegistry(t *testing.T, path string, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))
}

func TestLoadAccountsRegistry_ReadsTagToConfigDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	writeRegistry(t, path, `{
		"accounts": {
			"main": {"config_dir": "/Users/allencoates/.claude", "plan": "personal"},
			"team-a": {"config_dir": "/Users/allencoates/.claude-team-a", "plan": "team seat"}
		}
	}`)
	t.Setenv(AccountsRegistryEnvVar, path)

	registry := LoadAccountsRegistry()
	require.Equal(t, "/Users/allencoates/.claude", registry["main"])
	require.Equal(t, "/Users/allencoates/.claude-team-a", registry["team-a"])
	require.Len(t, registry, 2)
}

func TestLoadAccountsRegistry_MissingFileIsNotAnError(t *testing.T) {
	// log.Initialize is called so the loader's debug line (log.InfoLog) has
	// somewhere to write - the missing-registry branch is exercised here
	// exactly the way it will be in production, not skipped past it.
	log.Initialize(false)
	defer log.Close()

	t.Setenv(AccountsRegistryEnvVar, filepath.Join(t.TempDir(), "does-not-exist.json"))

	registry := LoadAccountsRegistry()
	require.Nil(t, registry)
}

func TestLoadAccountsRegistry_UnreadableJSONIsNotAnError(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	path := filepath.Join(t.TempDir(), "registry.json")
	writeRegistry(t, path, `not json`)
	t.Setenv(AccountsRegistryEnvVar, path)

	registry := LoadAccountsRegistry()
	require.Nil(t, registry)
}

func TestLoadAccountsRegistry_SkipsAccountWithEmptyConfigDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	writeRegistry(t, path, `{"accounts": {"stub": {"config_dir": ""}, "real": {"config_dir": "/x"}}}`)
	t.Setenv(AccountsRegistryEnvVar, path)

	registry := LoadAccountsRegistry()
	require.Len(t, registry, 1)
	require.Equal(t, "/x", registry["real"])
}
