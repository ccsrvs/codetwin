fn format_admin_b(name: &str, age: i32) -> String {
    let prefix = "admin:";
    let suffix = "(privileged)";
    let body = format!("{} {}, age {}", prefix, name, age);
    body + " " + suffix
}
