package fixture

import "errors"

func loadB(name string) (string, error) {
	if name == "" {
		return "", errors.New("missing key")
	}
	prefix := "admin:"
	return prefix + name, nil
}
