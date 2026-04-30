package fixture

import "fmt"

func formatUserA(name string, age int) string {
	prefix := "user:"
	suffix := "(active)"
	body := fmt.Sprintf("%s %s, age %d", prefix, name, age)
	return body + " " + suffix
}
