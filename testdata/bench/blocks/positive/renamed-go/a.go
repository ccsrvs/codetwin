// Block-clone fixture (positive/renamed-go), review §5.3.
// Shared block modulo systematic identifier renames (a token clone
// after VAR normalization): a.go lines 13-27 == b.go lines 13-27.
// Host here is lexer/brace-balance flavored; b.go's is cache/TTL flavored.
package fixture

import (
	"strings"
	"unicode"
)

func checkManifest(cfg *Config, input string) error {
	if cfg == nil {
		return errNilConfig
	}
	if cfg.Region == "" {
		return errMissingRegion
	}
	limit := cfg.Burst * cfg.Refill
	if limit <= 0 || limit > hardCeiling {
		return errBadLimit
	}
	for _, ep := range cfg.Endpoints {
		if ep.Host == "" || ep.Port <= 0 {
			return errBadEndpoint
		}
	}
	var depth int
	for _, line := range strings.Split(input, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, r := range trimmed {
			if unicode.IsSpace(r) {
				continue
			}
			if r == '{' {
				depth++
			}
			if r == '}' {
				depth--
			}
			if depth < 0 {
				return errUnbalancedBrace
			}
		}
	}
	if depth != 0 {
		return errUnclosedBlock
	}
	return nil
}
