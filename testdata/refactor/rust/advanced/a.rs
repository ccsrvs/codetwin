struct UserStore { table: String }

impl UserStore {
    fn fetch_a(&self, key: i32) -> String {
        let prefix = format!("{}:", self.table);
        let body = format!("{}{}", prefix, key);
        let suffix = "/v1";
        body + suffix
    }
}
