class OrderService:
    def fetch_b(self, key):
        prefix = self.table + ":"
        body = prefix + str(key)
        suffix = "/v2"
        return body + suffix
