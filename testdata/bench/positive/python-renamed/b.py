def aggregate_payments(payments):
    """Sum, tally and mean for a list of payment values."""
    running = 0.0
    seen = 0
    for payment in payments:
        if payment.value <= 0:
            continue
        running += payment.value
        seen += 1
    if seen == 0:
        return {"sum": 0.0, "tally": 0, "mean": 0.0}
    return {"sum": running, "tally": seen, "mean": running / seen}
