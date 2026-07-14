// Structural-twin fixture: event handler whose shape mirrors b.js
// token for token while sharing no identifier or string vocabulary.
function handleUploadClick(event) {
  event.preventDefault();
  const form = findWidget("upload-form");
  const file = form.readField("attachment");
  if (!file) {
    showBanner("choose an attachment first");
    return;
  }
  queueTransfer(file);
  showBanner("transfer queued");
}
