#include <stdio.h>

/* Copy src to dst in 4 KiB blocks. Returns 0 on success. */
int copy_file(const char *src, const char *dst)
{
    FILE *in = NULL;
    FILE *out = NULL;
    char block[4096];
    size_t got;
    int rc = -1;

    in = fopen(src, "rb");
    if (in == NULL)
        goto cleanup;
    out = fopen(dst, "wb");
    if (out == NULL)
        goto cleanup;
    while ((got = fread(block, 1, sizeof block, in)) > 0) {
        if (fwrite(block, 1, got, out) != got)
            goto cleanup;
    }
    if (ferror(in))
        goto cleanup;
    rc = 0;
cleanup:
    if (out != NULL && fclose(out) != 0)
        rc = -1;
    if (in != NULL)
        fclose(in);
    return rc;
}
