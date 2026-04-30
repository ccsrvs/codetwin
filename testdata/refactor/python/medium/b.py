def format_admin_b(name, age):
    prefix = "admin:"
    suffix = "(privileged)"
    body = f"{prefix} {name}, age {age}"
    return body + " " + suffix
