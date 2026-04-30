public class B {
    public String formatAdminB(String name, int age) {
        String prefix = "admin:";
        String suffix = "(privileged)";
        String body = String.format("%s %s, age %d", prefix, name, age);
        return body + " " + suffix;
    }
}
