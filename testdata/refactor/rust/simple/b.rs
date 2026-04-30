fn price_with_tax_b(amount: f64) -> f64 {
    let rounded = (amount * 100.0).round() / 100.0;
    let tax = rounded * 0.085;
    let total = rounded + tax;
    (total * 100.0).round() / 100.0
}
