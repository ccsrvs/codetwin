#include <stdio.h>
#include <stdlib.h>

/* Read a whole file into a freshly allocated, NUL-terminated buffer. */
int load_config_a(const char *path, char **out)
{
    FILE *fp = NULL;
    char *data = NULL;
    long size;
    int rc = -1;

    fp = fopen(path, "rb");
    if (fp == NULL)
        goto done;
    if (fseek(fp, 0, SEEK_END) != 0)
        goto done;
    size = ftell(fp);
    if (size < 0 || fseek(fp, 0, SEEK_SET) != 0)
        goto done;
    data = malloc((size_t)size + 1);
    if (data == NULL)
        goto done;
    if (fread(data, 1, (size_t)size, fp) != (size_t)size)
        goto done;
    data[size] = '\0';
    *out = data;
    data = NULL;
    rc = 0;
done:
    free(data);
    if (fp != NULL)
        fclose(fp);
    return rc;
}
