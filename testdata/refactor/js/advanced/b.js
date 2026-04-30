class OrderService {
  fetchB(key) {
    const prefix = this.table + ":";
    const body = prefix + String(key);
    const suffix = "/v2";
    return body + suffix;
  }
}
