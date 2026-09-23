#include <stdio.h>
#include <string.h>

struct admin {
    const char *login;
    int privileged;
    int level;
};

int format_admin_b(const struct admin *a, char *out, size_t cap)
{
    const char *role;
    int n;

    if (a == NULL || out == NULL)
        return -1;
    role = a->privileged ? "(privileged)" : "(standard)";
    n = snprintf(out, cap, "admin:%s %s level=%d", a->login, role, a->level);
    if (n < 0 || (size_t)n >= cap)
        return -1;
    return n;
}
