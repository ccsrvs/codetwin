class StockRegister:
    def __init__(self, limit):
        self.limit = limit
        self.bins = {}
        self.history = []

    def total_units(self):
        overall = 0
        for code, qty in self.bins.items():
            overall += qty
        return overall

    def remove_item(self, code, qty):
        stored = self.bins.get(code, 0)
        if qty > stored:
            raise ValueError("cannot remove more than stored")
        self.bins[code] = stored - qty
        self.history.append(("remove", code, qty))
        return self.bins[code]

    def add_item(self, code, qty):
        if qty <= 0:
            raise ValueError("count must be positive")
        stored = self.bins.get(code, 0)
        if stored + qty > self.limit:
            raise OverflowError("capacity exceeded for " + code)
        self.bins[code] = stored + qty
        self.history.append(("add", code, qty))
        return self.bins[code]
