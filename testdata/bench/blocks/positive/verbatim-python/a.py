# Block-clone fixture (positive/verbatim-python), review §5.3.
# Shared verbatim block: a.py lines 9-20 == b.py lines 9-20.
# Host is CSV/report flavored; b.py's host is queue/batch flavored.

import csv


def build_report(records, out_path):
    if not records:
        return []
    cleaned = []
    for rec in records:
        key = rec.get("id")
        if key is None:
            continue
        label = str(rec.get("name", "")).strip()
        if not label:
            continue
        cleaned.append((key, label))
    cleaned.sort()
    with open(out_path, "w", newline="") as fh:
        writer = csv.writer(fh, delimiter="|")
        writer.writerow(["id", "label", "bucket"])
        wide = 0
        narrow = 0
        for key, label in cleaned:
            if len(label) > WIDE_THRESHOLD:
                bucket = "wide"
                wide += 1
            else:
                bucket = "narrow"
                narrow += 1
            writer.writerow([key, label, bucket])
        writer.writerow(["total", wide + narrow, ""])
    ratio = 100.0 * wide / max(1, wide + narrow)
    summary = "wrote %d wide (%0.1f%%), %d narrow" % (wide, ratio, narrow)
    print(summary)
    return summary
