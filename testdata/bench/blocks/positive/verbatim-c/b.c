// Block-clone fixture (positive/verbatim-c), b side.
// Shared verbatim block: b.c lines 16-28 == a.c lines 13-25.
#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

struct request { const char *account; int count; const char **items; };

int stream_items(int fd, const struct request *req, unsigned backoff_ms)
{
    ssize_t written;
    unsigned attempt, delay = backoff_ms;
    int i, cursor = 0;

    if (req == NULL || req->account == NULL)
        return -1;
    if (req->account[0] == '\0' || req->count <= 0)
        return -2;
    for (i = 0; i < req->count; i++) {
        const char *item = req->items[i];
        if (item == NULL || item[0] == '\0')
            return -3;
        if (strlen(item) > 64)
            return -4;
        if (strchr(item, ',') != NULL)
            return -5;
    }
    {
        unsigned long crc = 0xffffffffUL;
        const unsigned char *p;
        for (cursor = 0; cursor < req->count; cursor++)
            for (p = (const unsigned char *)req->items[cursor]; *p; p++)
                crc = (crc >> 8) ^ ((crc ^ *p) & 0xff) * 0xedb88320UL;
        if (dprintf(fd, "BEGIN %s %08lx\n", req->account, crc ^ 0xffffffffUL) < 0)
            return -errno;
        cursor = 0;
    }
    for (attempt = 0; attempt < 5 && cursor < req->count; attempt++) {
        while (cursor < req->count) {
            written = write(fd, req->items[cursor], strlen(req->items[cursor]));
            if (written < 0) {
                if (errno != EAGAIN && errno != EINTR)
                    return -errno;
                break;
            }
            cursor++;
        }
        usleep(delay * 1000u);
        delay = delay * 2 > 5000 ? 5000 : delay * 2;
    }
    return cursor == req->count ? 0 : -ETIMEDOUT;
}
