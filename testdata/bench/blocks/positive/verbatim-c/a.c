// Block-clone fixture (positive/verbatim-c), review §5.3.
// Shared verbatim block: a.c lines 13-25 == b.c lines 16-28.
// The hosts differ: a.c renders a histogram report, b.c streams items
// to a file descriptor with retries and backoff.
#include <stdio.h>
#include <string.h>

struct request { const char *account; int count; const char **items; };

int render_report(const struct request *req, FILE *out)
{
    int i, bucket, widest = 0, histogram[8] = {0};
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
    for (i = 0; i < req->count; i++) {
        bucket = (int)strlen(req->items[i]) / 8;
        histogram[bucket > 7 ? 7 : bucket]++;
    }
    for (bucket = 0; bucket < 8; bucket++)
        if (histogram[bucket] > widest)
            widest = histogram[bucket];
    fprintf(out, "Length histogram for %s (%d rows)\n", req->account, req->count);
    for (bucket = 0; bucket < 8; bucket++) {
        fprintf(out, "%3d-%-3d | ", bucket * 8, bucket * 8 + 7);
        for (i = 0; i < histogram[bucket] * 40 / (widest ? widest : 1); i++)
            fputc('#', out);
        fputc('\n', out);
    }
    {
        double mean = 0.0, spread = 0.0;
        for (i = 0; i < req->count; i++)
            mean += (double)strlen(req->items[i]) / req->count;
        for (i = 0; i < req->count; i++) {
            double d = (double)strlen(req->items[i]) - mean;
            spread += d * d / req->count;
        }
        fprintf(out, "mean %.2f, variance %.2f, modal bucket width %d\n",
                mean, spread, widest);
    }
    return fflush(out) == 0 ? widest : -6;
}
