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
	"encoding/json"
	"os"
	"path/filepath"
)

// AccountsRegistryEnvVar overrides the registry path. Set only for tests;
// the real CLI relies on DefaultAccountsRegistryPath.
const AccountsRegistryEnvVar = "CLARITY_ACCOUNTS_REGISTRY"

// DefaultAccountsRegistryPath is the registry slice 1 landed at.
const DefaultAccountsRegistryPath = "/Users/allencoates/.claude-accounts/registry.json"

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
