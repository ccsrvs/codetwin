async function fetchProfile(req, res) {
  const userId = req.params.id;
  const profile = await db.profiles.findById(userId);
  if (!profile) {
    res.status(404).json({ error: 'profile not found' });
    return;
  }
  res.json({ name: profile.name, avatar: profile.avatarUrl });
}
