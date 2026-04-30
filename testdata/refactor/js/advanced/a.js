class UserService {
  fetchA(key) {
    const prefix = this.table + ":";
    const body = prefix + String(key);
    const suffix = "/v1";
    return body + suffix;
  }
}
