# Aggregate cache pressure per shard for oversized entries.


def shard_pressure(entries, limit):
    heavy = [scale(e.size) for e in entries if e.size > limit]
    totals = {}
    order = []
    for s in heavy:
        if s in totals:
            continue
        totals[s] = s
        order.append(s)
    return order
