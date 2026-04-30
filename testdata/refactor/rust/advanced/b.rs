struct OrderStore { table: String }

impl OrderStore {
    fn fetch_b(&self, key: i32) -> String {
        let prefix = format!("{}:", self.table);
        let body = format!("{}{}", prefix, key);
        let suffix = "/v2";
        body + suffix
    }
}
