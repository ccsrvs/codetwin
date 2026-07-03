def clamped_total(values, limit):
    """Sum the positive values, capping each at limit."""
    total = 0.0
    for value in values:
        if value <= 0:
            continue
        if value > limit:
            value = limit
        total += value
    return total
