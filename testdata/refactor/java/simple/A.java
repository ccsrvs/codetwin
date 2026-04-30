public class A {
    public double priceWithTaxA(double amount) {
        double rounded = Math.round(amount * 100.0) / 100.0;
        double tax = rounded * 0.07;
        double total = rounded + tax;
        return Math.round(total * 100.0) / 100.0;
    }
}
