public class A {
    public String formatUserA(String name, int age) {
        String prefix = "user:";
        String suffix = "(active)";
        String body = String.format("%s %s, age %d", prefix, name, age);
        return body + " " + suffix;
    }
}
