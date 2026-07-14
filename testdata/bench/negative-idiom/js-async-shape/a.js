// Fetch a user profile, falling back to null on any transport error.
async function loadProfile(userId) {
  try {
    const res = await api.get(`/users/${userId}`);
    return normalize(res.data);
  } catch (err) {
    logger.warn('profile fetch failed', err);
    return null;
  }
}
