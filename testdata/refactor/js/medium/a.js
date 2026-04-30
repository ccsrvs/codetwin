function formatUserA(name, age) {
  const prefix = "user:";
  const suffix = "(active)";
  const body = `${prefix} ${name}, age ${age}`;
  return body + " " + suffix;
}
