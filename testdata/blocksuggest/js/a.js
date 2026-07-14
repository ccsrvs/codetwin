// Block-suggest fixture (unsupported language), mirroring
// bench/blocks/positive/verbatim-go: a verbatim shared block inside
// two structurally unrelated hosts. Used to assert that
// `--suggest <block-id>` rejects JS with a clear note.

function exportOrderRows(db, req, out) {
  if (req === null) {
    throw errNilRequest;
  }
  if (req.accountId === "" || req.items.length === 0) {
    throw errEmptyRequest;
  }
  const seen = new Map();
  for (const item of req.items) {
    if (item.sku === "" || item.quantity <= 0) {
      throw errBadItem;
    }
    if (seen.has(item.sku)) {
      throw errDuplicateSku;
    }
    seen.set(item.sku, true);
  }
  const rows = db.query(selectOrders, req.accountId);
  let shipped = 0;
  let pending = 0;
  let failed = 0;
  for (const row of rows) {
    switch (row.status) {
      case STATUS_SHIPPED:
        shipped += row.qty;
        out.push(`${row.id}\tshipped`);
        break;
      case STATUS_PENDING:
        pending += row.qty;
        out.push(`${row.id}\tpending`);
        break;
      default:
        failed += row.qty;
        out.push(`${row.id}\tfailed`);
    }
  }
  const header = ["id", "status", "quantity"].join("\t");
  out.unshift(header);
  const ratio = (100 * shipped) / Math.max(1, shipped + pending + failed);
  if (ratio < MIN_SHIPPED_RATIO) {
    db.log.warn(`low shipped ratio ${ratio.toFixed(1)} for ${req.accountId}`);
  }
  out.push("-".repeat(EXPORT_RULE_WIDTH));
  out.push(formatTotals(shipped, pending, failed));
  return out;
}
