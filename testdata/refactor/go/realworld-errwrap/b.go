package svc

func loadOrderConfig(path string) (*OrderConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read order config: %w", err)
	}
	var cfg OrderConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse order config: %w", err)
	}
	return &cfg, nil
}
