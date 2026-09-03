// Package clarity: this file reads the multi-account registry the front-door
// slices build on top of - a single JSON file mapping a seat tag ("main",
// "team-a", ...) to the Claude Code config directory that seat uses
// (CLARITY_CONFIG_DIR at launch, per design/cockpit-pane/FRONTDOOR-SPEC.md
// "Where the declaration lives"). Nothing else in this package reads the
// file directly - gauge.go and discover.go both go through
// LoadAccountsRegistry so there is exactly one parser to keep in step with
// the registry's shape.
package clarity

import (
	"claude-squad/log"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// AccountsRegistryEnvVar overrides the registry path. Set only for tests;
// the real CLI relies on DefaultAccountsRegistryPath.
const AccountsRegistryEnvVar = "CLARITY_ACCOUNTS_REGISTRY"

// DefaultAccountsRegistryPath is the registry slice 1 landed at.
const DefaultAccountsRegistryPath = "/Users/allencoates/.claude-accounts/registry.json"

// EnvUnsetPrefix is the environment-clearing prefix a declared-seat launch
// program carries ahead of CLAUDE_CONFIG_DIR, verbatim from scripts/clarity's
// own env_unset_prefix. A cockpit pane inherits the tmux server's
// environment, which carries the owner's shell ANTHROPIC_API_KEY (and any
// stray CLAUDE_CODE_* session variable); left set, that key outranks the
// seat's own claude.ai login (observed on the max-2 seat: "ANTHROPIC_API_KEY
// or another auth source is set and takes precedence over your claude.ai
// login"). Exported for slice 7's login program to reuse.
const EnvUnsetPrefix = "env -u ANTHROPIC_API_KEY -u CLAUDE_CODE_CHILD_SESSION -u CLAUDE_CODE_SESSION_ID -u CLAUDE_CODE_BRIDGE_SESSION_ID -u CLAUDE_CODE_MESSAGING_SOCKET -u CLAUDE_CODE_MESSAGING_TOKEN -u CLAUDE_CODE_SSE_PORT -u CLAUDE_CODE_ENTRYPOINT"

// registryAccount is the one field this package needs from an account's
// registry entry - the rest (plan, purpose, default_modality) belong to
// later slices and are left for encoding/json to discard.
type registryAccount struct {
	ConfigDir string `json:"config_dir"`
}

// registryFile mirrors the registry's top-level shape: {"accounts": {tag:
// {...}}}.
type registryFile struct {
	Accounts map[string]registryAccount `json:"accounts"`
}

// accountsRegistryPath returns the registry path, honouring
// AccountsRegistryEnvVar for tests.
func accountsRegistryPath() string {
	if p := os.Getenv(AccountsRegistryEnvVar); p != "" {
		return p
	}
	return DefaultAccountsRegistryPath
}

// LoadAccountsRegistry reads the registry into a tag -> config_dir map. A
// missing or unreadable registry is not an error - most machines and most
// of this package's own tests have none - it yields a nil map and one
// debug log line; log.InfoLog is nil until log.Initialize runs (true of
// every plain `go test` invocation), so the line is skipped rather than
// panicking a caller that never asked for logging.
func LoadAccountsRegistry() map[string]string {
	path := accountsRegistryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if log.InfoLog != nil {
			log.InfoLog.Printf("accounts registry: %s not read: %v", path, err)
		}
		return nil
	}

	var file registryFile
	if err := json.Unmarshal(data, &file); err != nil {
		if log.InfoLog != nil {
			log.InfoLog.Printf("accounts registry: %s not parsed: %v", path, err)
		}
		return nil
	}

	out := make(map[string]string, len(file.Accounts))
	for tag, acc := range file.Accounts {
		if acc.ConfigDir == "" {
			continue
		}
		out[tag] = acc.ConfigDir
	}
	return out
}

// SeatOAuthAccount is the presence-plus-two-fields view of a seat folder's
// own .claude.json (the CLI's own top-level config file, not this
// registry) that seat resolution rule (c) and the slice 5 fleet line both
// need: whether an "oauthAccount" object exists at all, and its
// organizationName and seatTier if so - never emailAddress, accountUuid or
// any token, per the owner's correction the field-name survey was scoped
// to (BRIEF-FRONTDOOR-3B.md). The struct below has no field for any of
// those three, so there is nothing for a caller to read even by accident.
type SeatOAuthAccount struct {
	Present          bool
	OrganizationName string
	SeatTier         string
}

// ReadSeatOAuthAccount reads configDir/.claude.json and reports whether it
// carries an oauthAccount object. A missing or unreadable file, or one with
// no oauthAccount key, is not an error - it reports Present: false, the
// same shape the default root's own config was surveyed as before this
// file was written.
func ReadSeatOAuthAccount(configDir string) SeatOAuthAccount {
	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		return SeatOAuthAccount{}
	}

	var file struct {
		OAuthAccount *struct {
			OrganizationName string `json:"organizationName"`
			SeatTier         string `json:"seatTier"`
		} `json:"oauthAccount"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return SeatOAuthAccount{}
	}
	if file.OAuthAccount == nil {
		return SeatOAuthAccount{}
	}
	return SeatOAuthAccount{
		Present:          true,
		OrganizationName: file.OAuthAccount.OrganizationName,
		SeatTier:         file.OAuthAccount.SeatTier,
	}
}

// RegistryAccount is the full per-seat entry the new-lane overlay's step-2
// picker needs (front-door slice 6) - config_dir plus default_modality,
// beyond the bare tag->config_dir map LoadAccountsRegistry returns for
// gauge.go/discover.go's narrower needs. An additive second read of the
// same file, kept separate so neither existing caller's return type moves.
type RegistryAccount struct {
	Tag             string
	ConfigDir       string
	DefaultModality string
}

// RegistryPolicy is the registry's top-level "policy" block - today just
// the seat a new lane pre-selects at step 2 (FRONTDOOR-SPEC.md "Step 2
// account": "pre-select the policy default_account and re-order nothing").
type RegistryPolicy struct {
	DefaultAccount string
}

type registryAccountFull struct {
	ConfigDir       string `json:"config_dir"`
	DefaultModality string `json:"default_modality"`
}

type registryPolicyFile struct {
	DefaultAccount string `json:"default_account"`
}

type registryFileFull struct {
	Accounts map[string]registryAccountFull `json:"accounts"`
	Policy   registryPolicyFile             `json:"policy"`
}

// LoadAccountsRegistryFull reads the registry's full per-seat shape plus its
// policy block, tag-sorted for a deterministic picker order. A missing or
// unreadable registry yields (nil, RegistryPolicy{}) - the same
// "not an error" contract LoadAccountsRegistry already carries.
func LoadAccountsRegistryFull() ([]RegistryAccount, RegistryPolicy) {
	path := accountsRegistryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if log.InfoLog != nil {
			log.InfoLog.Printf("accounts registry: %s not read: %v", path, err)
		}
		return nil, RegistryPolicy{}
	}

	var file registryFileFull
	if err := json.Unmarshal(data, &file); err != nil {
		if log.InfoLog != nil {
			log.InfoLog.Printf("accounts registry: %s not parsed: %v", path, err)
		}
		return nil, RegistryPolicy{}
	}

	tags := make([]string, 0, len(file.Accounts))
	for tag := range file.Accounts {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	out := make([]RegistryAccount, 0, len(tags))
	for _, tag := range tags {
		acc := file.Accounts[tag]
		if acc.ConfigDir == "" {
			continue
		}
		out = append(out, RegistryAccount{Tag: tag, ConfigDir: acc.ConfigDir, DefaultModality: acc.DefaultModality})
	}
	return out, RegistryPolicy{DefaultAccount: file.Policy.DefaultAccount}
}

// IsDefaultConfigDir reports whether configDir is this machine's default
// Claude Code config directory - gauge.go's DefaultClaudeProjectsRoot's own
// parent. The launch program string (research F11) never carries
// CLAUDE_CONFIG_DIR for this one; every other seat's does.
func IsDefaultConfigDir(configDir string) bool {
	return filepath.Clean(configDir) == filepath.Clean(filepath.Dir(DefaultClaudeProjectsRoot))
}

// keychainServicePrefix mirrors account_probe_verify.sh's own constant
// (scripts/account_probe_verify.sh:57): the harness's own keychain service
// name for a seat's OAuth credential entry.
const keychainServicePrefix = "Claude Code-credentials"

// keychainServiceName returns the exact keychain service name a seat's
// credential entry carries, front-door slice 7 item 6b. Derivation
// established by reading THIS machine's own keychain metadata (service
// names only, via `security dump-keychain`, never `-d` and never a value)
// and comparing them with a SHA-256 of each registered seat's config dir:
// the default config dir's own entry carries the bare prefix with no
// suffix ("Claude Code-credentials"), and every other seat's entry carries
// the prefix plus a dash and the first 4 bytes (8 hex characters) of
// sha256(configDir) - team-a, team-b and max-2 each matched their own
// registered config dir's hash exactly, zero false matches, against this
// machine's own keychain and registry (3 Sep 2026). This is the harness's
// own keying scheme, read as metadata, not invented here - the probe
// itself only greps the bare prefix fleet-wide (account_probe_verify.sh:85,
// KEYCHAIN_SERVICE_PREFIX ends in a dash, so it never matches the default
// seat's own suffix-less entry either), which is the bug this derivation
// fixes: every seat, main included, gets its own check.
func keychainServiceName(configDir string) string {
	if IsDefaultConfigDir(configDir) {
		return keychainServicePrefix
	}
	sum := sha256.Sum256([]byte(filepath.Clean(configDir)))
	return fmt.Sprintf("%s-%x", keychainServicePrefix, sum[:4])
}

// securityDumpKeychain runs `security dump-keychain`, resolved through PATH
// like account_probe_verify.sh's own `security` calls - swappable in tests
// by prepending a scratch directory holding a fake `security` script to
// PATH, never by touching the real keychain. No flag here is ever `-d`; the
// output is service-name metadata only.
var securityDumpKeychain = func() ([]byte, error) {
	return exec.Command("security", "dump-keychain").Output()
}

// HasCredentialStore is the presence-only check step 2's credential-store
// column and slice 7's `l` key both read (front-door slice 7 items 1 and
// 6b), mirroring account_probe_verify.sh's own presence test
// (scripts/account_probe_verify.sh:83-89) in the same order - a file-based
// store first, then the seat's own Keychain entry - except keyed per seat
// via keychainServiceName above rather than the probe's fleet-wide prefix
// grep. Never reads the store's value on either path; a security failure
// or missing binary reports false, not an error - there is nothing else
// this call is entitled to assume.
func HasCredentialStore(configDir string) bool {
	if _, err := os.Stat(filepath.Join(configDir, ".credentials.json")); err == nil {
		return true
	}
	out, err := securityDumpKeychain()
	if err != nil {
		return false
	}
	needle := `"svce"<blob>="` + keychainServiceName(configDir) + `"`
	return strings.Contains(string(out), needle)
}

// AccountFromLaneDir exposes discover.go's accountFromLaneDir (unexported,
// package-internal) to callers outside package clarity - main.go's
// clarity-attach command, which registers a declared lane under its own
// seat when the wrapper does not pass --account yet (FRONTDOOR-SPEC.md
// slice 4 item 3, BRIEF-FRONTDOOR-4.md).
func AccountFromLaneDir(lanePath string) string {
	return accountFromLaneDir(lanePath)
}

// ModalityFromLaneDir exposes discover.go's modalityFromLaneDir the same
// way, for the same caller.
func ModalityFromLaneDir(lanePath string) string {
	return modalityFromLaneDir(lanePath)
}
