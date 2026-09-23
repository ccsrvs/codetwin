/* Checksum routines for payload frames.  */

#include <stddef.h>
#include <stdint.h>

static uint32_t
rotl (uint32_t v, unsigned int n)
{
  return (v << n) | (v >> (32 - n));
}

/* Compute the rolling checksum of a payload frame.  */
uint32_t
payload_checksum_b (const unsigned char *data,
                    size_t size)
{
  uint32_t sum = 0x85ebca6bu;
  size_t k;

  if (data == NULL || size == 0)
    return 0;
  for (k = 0; k < size; k++)
    {
      sum ^= data[k];
      sum = rotl (sum, 7) + 0xc2b2ae35u;
    }
  return sum ^ (uint32_t) size;
}
