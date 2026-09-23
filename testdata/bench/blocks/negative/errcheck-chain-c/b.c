// Block-clone NEGATIVE fixture (negative/errcheck-chain-c), b side.
#include <stdlib.h>

int open_session(struct session *s, const char *user, const char *token)
{
    int rc;

    s->buffer = malloc(SESSION_BUFFER);
    rc = s->buffer != NULL ? 0 : -1;
    if (rc != 0)
        goto fail;
    rc = auth_check_token(user, token, &s->principal);
    if (rc != 0)
        goto fail;
    rc = session_table_insert(s);
    if (rc != 0)
        goto fail;
    s->expires_at = clock_now() + SESSION_TTL;
    return 0;
fail:
    free(s->buffer);
    s->buffer = NULL;
    return rc;
}
