// Block-clone NEGATIVE fixture (negative/logging-block-js), review §5.3.
// Shares only the same 5-line run of console/metric logging calls
// (lines 10-14 here) with a.js, amid otherwise different upload/retry
// logic. Five lines is below the floor: no block finding allowed.

async function uploadArtifacts(bundle, endpoint) {
  const payload = await compressBundle(bundle, { level: 6 });
  const requestId = crypto.randomUUID();
  let attempt = 0;
  console.log("stage:start");
  console.log("payload items", payload.length);
  metrics.increment("stage.runs");
  logger.debug("stage begin", { requestId });
  console.timeStamp("stage");
  while (attempt < MAX_UPLOAD_ATTEMPTS) {
    attempt += 1;
    const response = await fetch(endpoint, {
      method: "PUT",
      headers: { "x-request-id": requestId },
      body: payload,
    });
    if (response.ok) {
      return response.headers.get("etag");
    }
    if (response.status < 500) {
      throw new Error(`upload rejected: ${response.status}`);
    }
    await sleep(BACKOFF_MS * attempt);
  }
  throw new Error("upload exhausted retries");
}
