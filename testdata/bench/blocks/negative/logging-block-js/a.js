// Block-clone NEGATIVE fixture (negative/logging-block-js), review §5.3.
// The two functions share only a 5-line run of console/metric logging
// calls (lines 10-14 here) amid otherwise different logic. Five lines is
// below the min-block-lines floor: no block finding may be produced.

function renderCheckout(cart, mount) {
  const payload = cart.items.filter((item) => item.quantity > 0);
  const requestId = cart.session ? cart.session.id : "anon";
  const fragment = document.createDocumentFragment();
  console.log("stage:start");
  console.log("payload items", payload.length);
  metrics.increment("stage.runs");
  logger.debug("stage begin", { requestId });
  console.timeStamp("stage");
  let subtotal = 0;
  for (const item of payload) {
    const row = document.createElement("li");
    row.textContent = `${item.name} × ${item.quantity}`;
    row.dataset.sku = item.sku;
    fragment.appendChild(row);
    subtotal += item.quantity * item.unitPrice;
  }
  const footer = document.createElement("p");
  footer.textContent = formatCurrency(subtotal, cart.currency);
  fragment.appendChild(footer);
  mount.replaceChildren(fragment);
  return subtotal;
}
