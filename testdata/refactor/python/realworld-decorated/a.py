@retry(attempts=3)
@cached
def load_user_profile(user_id):
    record = db.users.get(user_id)
    enriched = enrich_with_metadata(record)
    audit.log("read_user", user_id=user_id)
    return enriched
