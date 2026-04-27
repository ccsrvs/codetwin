async function fetchUser(id) {
  const response = await fetch('/api/users/' + id);
  const data = await response.json();
  return data;
}