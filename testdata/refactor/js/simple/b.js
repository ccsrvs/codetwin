function priceWithTaxB(amount) {
  const rounded = Math.round(amount * 100) / 100;
  const tax = rounded * 0.085;
  const total = rounded + tax;
  return Math.round(total * 100) / 100;
}
