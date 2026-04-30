function processA(input) {
  log.info("processing: " + input);
  cache.set(input, true);
  metric.increment();
}
