@retry(attempts=5)
def load_order_profile(order_id):
    record = db.orders.get(order_id)
    enriched = enrich_with_metadata(record)
    audit.log("read_order", order_id=order_id)
    return enriched
