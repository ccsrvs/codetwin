// Structural-twin fixture: same handler shape as a.js, fully
// disjoint vocabulary (different helpers, fields, and messages).
function onFilterChange(evt) {
  evt.stopPropagation();
  const panel = locateSection("filter-panel");
  const range = panel.currentValue("price");
  if (!range) {
    paintNotice("pick a price range");
    return;
  }
  refreshListing(range);
  paintNotice("listing refreshed");
}
