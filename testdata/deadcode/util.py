def _dead_python_helper(items):
    seen = set()
    result = []
    for item in items:
        if item not in seen:
            seen.add(item)
            result.append(item)
    return result


def used_python_helper(items):
    counts = {}
    for item in items:
        counts[item] = counts.get(item, 0) + 1
    return counts


def python_caller(items):
    tally = used_python_helper(items)
    return sorted(tally)


if __name__ == "__main__":
    python_caller(["a", "b", "a"])
