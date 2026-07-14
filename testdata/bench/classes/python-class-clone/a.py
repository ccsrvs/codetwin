class InventoryLedger:
    def __init__(self, capacity):
        self.capacity = capacity
        self.items = {}
        self.audit_log = []

    def add_item(self, sku, count):
        if count <= 0:
            raise ValueError("count must be positive")
        current = self.items.get(sku, 0)
        if current + count > self.capacity:
            raise OverflowError("capacity exceeded for " + sku)
        self.items[sku] = current + count
        self.audit_log.append(("add", sku, count))
        return self.items[sku]

    def remove_item(self, sku, count):
        current = self.items.get(sku, 0)
        if count > current:
            raise ValueError("cannot remove more than stored")
        self.items[sku] = current - count
        self.audit_log.append(("remove", sku, count))
        return self.items[sku]

    def total_units(self):
        total = 0
        for sku, count in self.items.items():
            total += count
        return total
