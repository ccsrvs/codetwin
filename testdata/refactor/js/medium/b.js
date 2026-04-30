function formatAdminB(name, age) {
  const prefix = "admin:";
  const suffix = "(privileged)";
  const body = `${prefix} ${name}, age ${age}`;
  return body + " " + suffix;
}
