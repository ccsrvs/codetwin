public class A {
    public void process(String input) {
        log.info("processing: " + input);
        cache.put(input, true);
        metric.increment();
    }
}
