#include <stdio.h>

/* Duplicate a snapshot file; 0 means the copy is complete. */
int clone_snapshot(const char *from, const char *to)
{
    FILE *reader = NULL;
    FILE *writer = NULL;
    char chunk[8192];
    size_t n;
    int status = -1;

    reader = fopen(from, "rb");
    if (reader == NULL)
        goto fail;
    writer = fopen(to, "wb");
    if (writer == NULL)
        goto fail;
    while ((n = fread(chunk, 1, sizeof chunk, reader)) > 0) {
        if (fwrite(chunk, 1, n, writer) != n)
            goto fail;
    }
    if (ferror(reader))
        goto fail;
    status = 0;
fail:
    if (writer != NULL && fclose(writer) != 0)
        status = -1;
    if (reader != NULL)
        fclose(reader);
    return status;
}
