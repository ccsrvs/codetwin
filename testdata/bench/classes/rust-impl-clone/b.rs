pub struct TradeLedger {
    levels: usize,
    close_price: f64,
}

impl TradeLedger {
    pub fn levels_remaining(&self, used: usize) -> usize {
        if used > self.levels {
            return 0;
        }
        self.levels - used
    }

    pub fn record_fill(&mut self, price: f64, size: i64) -> f64 {
        if size <= 0 {
            panic!("size must be positive");
        }
        let notional = price * size as f64;
        self.close_price = notional / size as f64;
        notional
    }

    pub fn mid(&self, bid: f64, ask: f64) -> f64 {
        let spread = ask - bid;
        if spread < 0.0 {
            panic!("crossed book");
        }
        bid + spread / 2.0
    }
}
