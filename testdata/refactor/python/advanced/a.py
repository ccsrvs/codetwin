class UserService:
    def fetch_a(self, key):
        prefix = self.table + ":"
        body = prefix + str(key)
        suffix = "/v1"
        return body + suffix
