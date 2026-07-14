public class QuoteLedger {
    private final int levels;
    private double closePrice;

    public QuoteLedger(int levels) {
        this.levels = levels;
        this.closePrice = 0.0;
    }

    public double midpoint(double buy, double sell) {
        double gap = sell - buy;
        if (gap < 0) {
            throw new IllegalArgumentException("crossed book");
        }
        return buy + gap / 2.0;
    }

    public double recordTrade(double quote, int volume) {
        if (volume <= 0) {
            throw new IllegalArgumentException("size must be positive");
        }
        double notional = quote * volume;
        closePrice = notional / volume;
        return notional;
    }
}
