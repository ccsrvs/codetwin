# Block-clone NEGATIVE fixture (negative/import-adjacent-python), §5.3.
# Shares only the same trivial file-handling/dict-init setup lines with
# a.py; the actual logic (numeric column aggregation) is unrelated.
# No block finding may be produced at min-block-lines 8.


def column_extrema(path, column_index):
    handle = open(path, "r", encoding="utf-8")
    lines = handle.readlines()
    handle.close()
    stats = {}
    lowest = None
    highest = None
    for row_number, line in enumerate(lines):
        cells = line.rstrip("\n").split("\t")
        if column_index >= len(cells):
            stats.setdefault("short_rows", []).append(row_number)
            continue
        try:
            value = float(cells[column_index])
        except ValueError:
            stats.setdefault("bad_cells", []).append(row_number)
            continue
        if lowest is None or value < lowest:
            lowest = value
        if highest is None or value > highest:
            highest = value
    return lowest, highest, stats
