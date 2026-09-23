// Block-clone NEGATIVE fixture (negative/errcheck-chain-c), review §5.3.
// The only cross-file commonality is C error-check boilerplate:
// `if (rc != 0) goto fail;` stanzas after calls of different shapes.
// No verbatim run reaches 8 lines, so no block may be reported.
#include <stdlib.h>

int init_device(struct device *dev, const struct config *cfg)
{
    int rc;

    rc = device_power_on(dev);
    if (rc != 0)
        goto fail;
    rc = device_set_clock(dev, cfg->clock_hz);
    if (rc != 0)
        goto fail;
    rc = device_load_firmware(dev, cfg->firmware_path);
    if (rc != 0)
        goto fail;
    dev->state = DEVICE_READY;
    dev->retries = cfg->retries;
    log_info("device %s ready at %u Hz", dev->name, cfg->clock_hz);
    return 0;
fail:
    device_power_off(dev);
    return rc;
}
