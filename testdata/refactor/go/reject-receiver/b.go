package fixture

type OrderRepo struct{ table string }

func (r *OrderRepo) FindB(id int) string {
	prefix := r.table + ":"
	return prefix + itoa(id)
}
