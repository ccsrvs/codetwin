def load_config(path):
    """Read key=value lines into a dict, ignoring comments."""
    settings = {}
    with open(path) as fh:
        for line in fh:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            key, _, value = line.partition("=")
            settings[key.strip()] = value.strip()
    return settings
