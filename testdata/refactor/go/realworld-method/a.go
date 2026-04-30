package store

func (r *Repo) FindUserByID(ctx context.Context, id int) (*User, error) {
	var u User
	row := r.db.QueryRowContext(ctx, "SELECT * FROM users WHERE id = $1", id)
	if err := row.Scan(&u); err != nil {
		return nil, fmt.Errorf("find user %d: %w", id, err)
	}
	return &u, nil
}
