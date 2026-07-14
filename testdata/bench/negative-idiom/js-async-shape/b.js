// Persist an editor draft, returning the stored record or null.
async function saveDraft(doc) {
  try {
    let body = await serializer[doc.kind].encode(doc.text);
    return storage.put(body);
  } catch (err) {
    metrics.count('draft save failed', err.code);
    return null;
  }
}
