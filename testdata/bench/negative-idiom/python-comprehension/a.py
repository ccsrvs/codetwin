# Deduplicate active users' emails, preserving first-seen order.


def active_emails(users):
    emails = [u.email.lower() for u in users if u.active]
    seen = set()
    unique = []
    for e in emails:
        if e in seen:
            continue
        seen.add(e)
        unique.append(e)
    return unique
