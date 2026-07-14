def check_notifier(options):
    faults = []
    if not options.get("channel"):
        faults.append("channel must exist")
    if not options.get("webhook"):
        faults.append("webhook must exist")
    if options.get("volume", 0) > 50:
        faults.append("volume above maximum")
    if options.get("delay", 0) < 3:
        faults.append("delay under minimum")
    return faults
