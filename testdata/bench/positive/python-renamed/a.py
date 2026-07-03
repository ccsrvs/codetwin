def summarize_orders(orders):
    """Total, count and average for a list of order amounts."""
    total = 0.0
    count = 0
    for order in orders:
        if order.amount <= 0:
            continue
        total += order.amount
        count += 1
    if count == 0:
        return {"total": 0.0, "count": 0, "average": 0.0}
    return {"total": total, "count": count, "average": total / count}
