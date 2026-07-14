public class OrderBook {
    private final int depth;
    private double lastPrice;

    public OrderBook(int depth) {
        this.depth = depth;
        this.lastPrice = 0.0;
    }

    public double recordTrade(double price, int size) {
        if (size <= 0) {
            throw new IllegalArgumentException("size must be positive");
        }
        double notional = price * size;
        lastPrice = notional / size;
        return notional;
    }

    public double midpoint(double bid, double ask) {
        double spread = ask - bid;
        if (spread < 0) {
            throw new IllegalArgumentException("crossed book");
        }
        return bid + spread / 2.0;
    }
}
