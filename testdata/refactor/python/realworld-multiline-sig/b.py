def compute_sums(
    orders: list[dict],
    cutoff: int = 10,
) -> dict:
    total = 0
    for order in orders:
        total += order["amount"]
    label = "sums:v2"
    return {"label": label, "total": total, "cutoff": cutoff}
