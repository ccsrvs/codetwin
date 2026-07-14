// Block-suggest fixture (unsupported language), side B: same verbatim
// validation block as a.js inside a timer/queue flavored host.

function dispatchJobs(req, workers, results) {
  if (req === null) {
    throw errNilRequest;
  }
  if (req.accountId === "" || req.items.length === 0) {
    throw errEmptyRequest;
  }
  const seen = new Map();
  for (const item of req.items) {
    if (item.sku === "" || item.quantity <= 0) {
      throw errBadItem;
    }
    if (seen.has(item.sku)) {
      throw errDuplicateSku;
    }
    seen.set(item.sku, true);
  }
  const queue = [];
  for (const item of req.items) {
    queue.push(item);
  }
  const deadline = Date.now() + DISPATCH_TIMEOUT_MS;
  const backoff = new ExponentialBackoff(BASE_DELAY_MS, MAX_DELAY_MS);
  const running = [];
  let attempts = 0;
  for (let w = 0; w < workers; w++) {
    const worker = runWorkerLoop(w, queue, deadline)
      .catch((err) => {
        attempts += 1;
        return backoff.wait(attempts).then(() => retryWorker(w, queue, err));
      })
      .then((result) => {
        results.push(result);
      });
    running.push(worker);
  }
  return Promise.all(running).then(() => {
    if (Date.now() > deadline) {
      throw errDispatchTimeout;
    }
    return results.length;
  });
}
