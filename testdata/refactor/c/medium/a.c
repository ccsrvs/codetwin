#include <stdio.h>

struct record {
    const char *name;
    int active;
    int count;
};

int format_user_a(const struct record *r, char *buf, size_t len)
{
    const char *status;
    int n;

    if (r == NULL || buf == NULL)
        return -1;
    status = r->active ? "(active)" : "(inactive)";
    n = snprintf(buf, len, "user:%s %s count=%d", r->name, status, r->count);
    if (n < 0 || (size_t)n >= len)
        return -1;
    return n;
}
