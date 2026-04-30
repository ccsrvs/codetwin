package fixture

import "fmt"

func formatAdminB(name string, age int) string {
	prefix := "admin:"
	suffix := "(privileged)"
	body := fmt.Sprintf("%s %s, age %d", prefix, name, age)
	return body + " " + suffix
}
