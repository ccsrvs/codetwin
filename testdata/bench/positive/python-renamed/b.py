def aggregate_payments(payments):
    """Total, count and average for a list of payment values."""
    running = 0.0
    seen = 0
    for payment in payments:
        if payment.value <= 0:
            continue
        running += payment.value
        seen += 1
    if seen == 0:
        return {"total": 0.0, "count": 0, "average": 0.0}
    return {"total": running, "count": seen, "average": running / seen}
