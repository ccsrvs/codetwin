// Block-clone NEGATIVE fixture (negative/errcheck-chain-go), review §5.3.
// The only cross-file commonality with a.go is Go error-check
// boilerplate: four consecutive `if err != nil { return err }` stanzas
// whose initializer shapes differ (and appear in a different order
// than a.go's). No block finding may be produced at min-block-lines 8.
package fixture

import "fmt"

func applyRuntimeConfig(target *Config, path string, defaults Overlay) error {
	if err := defaults.Validate(path); err != nil {
		return err
	}
	raw, err := readConfigFile(path)
	if err != nil {
		return err
	}
	if err := unmarshalStrict(raw, target, defaults.LintMode); err != nil {
		return err
	}
	if err := target.watcher.Flush(); err != nil {
		return err
	}
	for section, overlay := range defaults.Sections {
		if _, taken := target.Sections[section]; taken {
			continue
		}
		target.Sections[section] = overlay
	}
	target.origin = fmt.Sprintf("%s@%d", path, defaults.Revision)
	return nil
}
