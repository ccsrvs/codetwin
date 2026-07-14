# Block-clone fixture (positive/verbatim-python), review §5.3.
# Shared verbatim block: b.py lines 9-20 == a.py lines 9-20.
# Host is queue/batch flavored; a.py's host is CSV/report flavored.

import collections


def drain_into_batches(records, enqueue, requeue):
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
    pending = collections.deque(cleaned)
    batches = []
    while pending:
        batch = []
        while pending and len(batch) < BATCH_SIZE:
            batch.append(pending.popleft())
        batches.append(batch)
    dispatched = 0
    for index, batch in enumerate(batches):
        ticket = enqueue(batch, priority=index == 0)
        if ticket is None:
            requeue(batch)
            continue
        dispatched += len(batch)
    backlog = len(cleaned) - dispatched
    if backlog > MAX_SILENT_BACKLOG:
        alert_backlog(backlog, source="drain")
    return dispatched
