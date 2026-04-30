def format_user_a(name, age):
    prefix = "user:"
    suffix = "(active)"
    body = f"{prefix} {name}, age {age}"
    return body + " " + suffix
