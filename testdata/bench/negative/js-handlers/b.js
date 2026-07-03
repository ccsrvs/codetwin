async function deleteSession(req, res) {
  const token = req.headers.authorization;
  const session = await sessions.lookup(token);
  if (session) {
    await sessions.revoke(session.id);
    await audit.log('logout', session.userId);
  }
  res.status(204).end();
}
