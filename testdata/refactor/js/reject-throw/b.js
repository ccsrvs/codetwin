function processB(input) {
  log.info("processing: " + input);
  cache.set(input, true);
  throw new Error("bad state");
}
