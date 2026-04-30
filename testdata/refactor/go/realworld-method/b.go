package store

func (r *Repo) FindOrderByID(ctx context.Context, id int) (*Order, error) {
	var o Order
	row := r.db.QueryRowContext(ctx, "SELECT * FROM orders WHERE id = $1", id)
	if err := row.Scan(&o); err != nil {
		return nil, fmt.Errorf("find order %d: %w", id, err)
	}
	return &o, nil
}
