public class B {
    public static class Inner {
        public double priceWithTaxB(double amount) {
            double rounded = Math.round(amount * 100.0) / 100.0;
            double tax = rounded * 0.085;
            double total = rounded + tax;
            return Math.round(total * 100.0) / 100.0;
        }
    }
}
