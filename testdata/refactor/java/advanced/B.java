public class OrderStore {
    private String table;
    public String fetchB(int key) {
        String prefix = this.table + ":";
        String body = prefix + Integer.toString(key);
        String suffix = "/v2";
        return body + suffix;
    }
}
