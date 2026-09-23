#include <stdio.h>
#include <string.h>

struct user {
    const char *name;
    int active;
    int age;
};

int format_user_a(const struct user *u, char *buf, size_t len)
{
    const char *status;
    int n;

    if (u == NULL || buf == NULL)
        return -1;
    status = u->active ? "(active)" : "(inactive)";
    n = snprintf(buf, len, "user:%s %s age=%d", u->name, status, u->age);
    if (n < 0 || (size_t)n >= len)
        return -1;
    return n;
}
