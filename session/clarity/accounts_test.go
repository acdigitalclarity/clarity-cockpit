package clarity

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"claude-squad/log"

	"github.com/stretchr/testify/require"
)

// fakeSecurityOnPath puts a fake `security` script ahead of the real one on
// PATH, so HasCredentialStore's own `security dump-keychain` call never
// touches this machine's real keychain - the account_probe_verify.sh idiom
// item 4a asks for. output is written verbatim to stdout, one line per
// keychain entry; t.Setenv restores PATH on cleanup.
func fakeSecurityOnPath(t *testing.T, output string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake security script is a POSIX shell script")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\ncat <<'EOF'\n" + output + "\nEOF\n"
	path := filepath.Join(bin, "security")
	require.NoError(t, os.WriteFile(path, []byte(script), 0755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// keychainSuffixHex mirrors keychainServiceName's own non-default branch so
// tests can build a matching fake entry without reaching into the
// unexported function directly.
func keychainSuffixHex(configDir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(configDir)))
	return fmt.Sprintf("%x", sum[:4])
}

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

func TestLoadAccountsRegistryFull_ReadsAccountsSortedByTagAndPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	writeRegistry(t, path, `{
		"accounts": {
			"team-b": {"config_dir": "/Users/allencoates/.claude-team-b", "default_modality": ""},
			"main": {"config_dir": "/Users/allencoates/.claude", "default_modality": ""},
			"team-a": {"config_dir": "/Users/allencoates/.claude-team-a", "default_modality": "app-pipeline"},
			"stub": {"config_dir": ""}
		},
		"policy": {"default_account": "main", "note": "ignored"}
	}`)
	t.Setenv(AccountsRegistryEnvVar, path)

	accounts, policy := LoadAccountsRegistryFull()
	require.Equal(t, "main", policy.DefaultAccount)
	require.Len(t, accounts, 3, "the empty-config_dir stub account must be skipped, same as LoadAccountsRegistry")
	require.Equal(t, []string{"main", "team-a", "team-b"}, []string{accounts[0].Tag, accounts[1].Tag, accounts[2].Tag},
		"accounts must come back tag-sorted for a deterministic picker order")
	require.Equal(t, "app-pipeline", accounts[1].DefaultModality)
}

func TestLoadAccountsRegistryFull_MissingFileIsNotAnError(t *testing.T) {
	t.Setenv(AccountsRegistryEnvVar, filepath.Join(t.TempDir(), "does-not-exist.json"))

	accounts, policy := LoadAccountsRegistryFull()
	require.Nil(t, accounts)
	require.Equal(t, "", policy.DefaultAccount)
}

func TestIsDefaultConfigDir(t *testing.T) {
	require.True(t, IsDefaultConfigDir(filepath.Dir(DefaultClaudeProjectsRoot)))
	require.False(t, IsDefaultConfigDir("/Users/allencoates/.claude-team-b"))
}

// TestReadSeatOAuthAccount_PresentReportsOrgAndTierOnly proves the field
// fence itself: even when the source .claude.json carries accountUuid and
// emailAddress, the struct ReadSeatOAuthAccount returns has no field for
// either - there is nothing for a caller to read even by accident.
func TestReadSeatOAuthAccount_PresentReportsOrgAndTierOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude.json"),
		[]byte(`{"oauthAccount":{"organizationName":"Digital Clarity","organizationType":"claude_team","seatTier":"team_tier_1","accountUuid":"must-never-surface","emailAddress":"must-never-surface"}}`), 0644))

	got := ReadSeatOAuthAccount(dir)
	require.True(t, got.Present)
	require.Equal(t, "Digital Clarity", got.OrganizationName)
	require.Equal(t, "team_tier_1", got.SeatTier)
}

func TestReadSeatOAuthAccount_MissingFileIsNotAnError(t *testing.T) {
	got := ReadSeatOAuthAccount(t.TempDir())
	require.False(t, got.Present)
}

func TestReadSeatOAuthAccount_NoOAuthAccountKeyIsAbsent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"someOtherKey":true}`), 0644))

	got := ReadSeatOAuthAccount(dir)
	require.False(t, got.Present)
}

func TestReadSeatOAuthAccount_UnparseableJSONIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude.json"), []byte("not json"), 0644))

	got := ReadSeatOAuthAccount(dir)
	require.False(t, got.Present)
}

// (4a) HasCredentialStore reports false for a fresh scratch dir with no
// file store and an empty keychain, and true once account_probe_verify.sh's
// own presence marker is present - the file-based branch, needing no fake
// security at all.
func TestHasCredentialStore_FreshDirIsFalse(t *testing.T) {
	dir := t.TempDir()
	fakeSecurityOnPath(t, "")
	require.False(t, HasCredentialStore(dir), "a fresh scratch dir with an empty keychain must report no store")
}

func TestHasCredentialStore_CredentialsFileIsTrue(t *testing.T) {
	dir := t.TempDir()
	fakeSecurityOnPath(t, "") // must never even need to be read - the file wins first
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte("{}"), 0600))
	require.True(t, HasCredentialStore(dir))
}

// (4a + 6b) the Keychain branch, keyed per seat: a fake `security dump-
// keychain` printing THIS seat's own derived entry name reports true; the
// SAME fake printing only ANOTHER seat's entry (or the bare default-seat
// entry) reports false for this seat - proving the check reads its own
// seat's entry, not just any entry on the keychain.
func TestHasCredentialStore_KeychainEntry_KeyedPerSeat(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".claude-scratch-seat")
	require.NoError(t, os.MkdirAll(dir, 0755))
	suffix := keychainSuffixHex(dir)

	fakeSecurityOnPath(t, fmt.Sprintf(`"svce"<blob>="Claude Code-credentials-%s"`, suffix))
	require.True(t, HasCredentialStore(dir), "this seat's own derived keychain entry must report present")

	otherDir := filepath.Join(t.TempDir(), ".claude-other-seat")
	require.NoError(t, os.MkdirAll(otherDir, 0755))
	require.NotEqual(t, suffix, keychainSuffixHex(otherDir), "the two scratch dirs must hash to different suffixes for this control to mean anything")
	require.False(t, HasCredentialStore(otherDir), "another seat's entry on the keychain must never read as presence for THIS seat")

	fakeSecurityOnPath(t, `"svce"<blob>="Claude Code-credentials"`) // the bare default-seat entry only
	require.False(t, HasCredentialStore(dir), "the default seat's bare entry must never read as presence for a non-default seat")
}

// (6b) the primary seat (config dir the default ~/.claude) is keyed to the
// BARE prefix with no hash suffix - the derivation this leg established by
// reading this machine's own keychain metadata (session/clarity/accounts.go
// keychainServiceName's own doc comment). A suffixed entry for some OTHER
// seat must never read as presence for main.
func TestHasCredentialStore_DefaultSeat_BareEntryKeyedSeparately(t *testing.T) {
	defaultDir := filepath.Dir(DefaultClaudeProjectsRoot)

	fakeSecurityOnPath(t, `"svce"<blob>="Claude Code-credentials"`)
	require.True(t, HasCredentialStore(defaultDir), "the default seat's own bare keychain entry must report present")

	fakeSecurityOnPath(t, `"svce"<blob>="Claude Code-credentials-deadbeef"`)
	require.False(t, HasCredentialStore(defaultDir), "another seat's suffixed entry must never read as presence for the default seat")
}
