package fixture

type UserRepo struct{ table string }

func (r *UserRepo) FindA(id int) string {
	prefix := r.table + ":"
	return prefix + itoa(id)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return ""
}
