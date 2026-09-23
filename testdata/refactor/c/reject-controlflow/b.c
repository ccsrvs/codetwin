#include <stdlib.h>

int parse_port_b(const char *s)
{
    long v;
    char *end;

    v = strtol(s, &end, 10);
    if (*end != '\0')
        v = 0;
    return (int)v;
}
