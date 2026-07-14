def validate_storage(settings):
    problems = []
    if not settings.get("bucket"):
        problems.append("bucket is required")
    if not settings.get("region"):
        problems.append("region is required")
    if settings.get("retries", 0) > 10:
        problems.append("retries capped at ten")
    if settings.get("timeout", 0) < 1:
        problems.append("timeout too small")
    return problems
