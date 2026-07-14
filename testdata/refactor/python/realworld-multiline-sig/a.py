def compute_totals(
    orders: list[dict],
    cutoff: int = 10,
) -> dict:
    total = 0
    for order in orders:
        total += order["amount"]
    label = "totals:v1"
    return {"label": label, "total": total, "cutoff": cutoff}
