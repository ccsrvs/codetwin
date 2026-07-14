pub struct OrderBook {
    depth: usize,
    last_price: f64,
}

impl OrderBook {
    pub fn record_trade(&mut self, price: f64, size: i64) -> f64 {
        if size <= 0 {
            panic!("size must be positive");
        }
        let notional = price * size as f64;
        self.last_price = notional / size as f64;
        notional
    }

    pub fn midpoint(&self, bid: f64, ask: f64) -> f64 {
        let spread = ask - bid;
        if spread < 0.0 {
            panic!("crossed book");
        }
        bid + spread / 2.0
    }

    pub fn depth_remaining(&self, used: usize) -> usize {
        if used > self.depth {
            return 0;
        }
        self.depth - used
    }
}
