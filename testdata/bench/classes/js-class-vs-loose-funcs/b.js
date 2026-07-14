function subtotal(items) {
  let total = 0;
  for (const item of items) {
    const line = item.price * item.qty;
    if (line < 0) {
      throw new Error("negative line total");
    }
    total += line;
  }
  return total;
}

function taxFor(amount, rate) {
  if (rate < 0 || rate > 1) {
    throw new Error("rate out of range");
  }
  const raw = amount * rate;
  const cents = Math.round(raw * 100);
  const scaled = cents / 100;
  return scaled;
}
