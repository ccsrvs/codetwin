/* Checksum routines for the packet layer.  */

#include <stddef.h>
#include <stdint.h>

static uint32_t
rotl (uint32_t v, unsigned int n)
{
  return (v << n) | (v >> (32 - n));
}

/* Compute the rolling checksum of a header block.
   Returns 0 for an empty block.  */
uint32_t
header_checksum_a (const unsigned char *buf,
                   size_t len)
{
  uint32_t sum = 0x9e3779b9u;
  size_t i;

  if (buf == NULL || len == 0)
    return 0;
  for (i = 0; i < len; i++)
    {
      sum ^= buf[i];
      sum = rotl (sum, 5) + 0x7f4a7c15u;
    }
  return sum ^ (uint32_t) len;
}
