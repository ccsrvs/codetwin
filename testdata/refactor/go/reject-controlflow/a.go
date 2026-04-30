package fixture

import "errors"

func loadA(name string) (string, error) {
	if name == "" {
		return "", errors.New("empty name")
	}
	prefix := "user:"
	return prefix + name, nil
}
