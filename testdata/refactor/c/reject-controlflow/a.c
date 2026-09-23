#include <stdlib.h>

int parse_port_a(const char *s)
{
    long v;
    char *end;

    v = strtol(s, &end, 10);
    if (*end != '\0')
        return -1;
    return (int)v;
}
