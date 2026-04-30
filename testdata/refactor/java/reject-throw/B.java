public class B {
    public void process(String input) {
        log.info("processing: " + input);
        cache.put(input, true);
        throw new IllegalStateException("bad state");
    }
}
