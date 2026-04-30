public class UserStore {
    private String table;
    public String fetchA(int key) {
        String prefix = this.table + ":";
        String body = prefix + Integer.toString(key);
        String suffix = "/v1";
        return body + suffix;
    }
}
