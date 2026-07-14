# Block-clone NEGATIVE fixture (negative/import-adjacent-python), §5.3.
# The two functions share only trivial file-handling/dict-init setup
# boilerplate (4 short lines) amid entirely different logic. No block
# finding may be produced at min-block-lines 8.


def word_frequency(path, min_length):
    handle = open(path, "r", encoding="utf-8")
    lines = handle.readlines()
    handle.close()
    stats = {}
    for line in lines:
        for token in line.lower().split():
            word = token.strip(".,;:!?\"'()")
            if len(word) < min_length:
                continue
            stats[word] = stats.get(word, 0) + 1
    ranked = sorted(stats.items(), key=lambda kv: (-kv[1], kv[0]))
    return ranked[:TOP_WORDS]
