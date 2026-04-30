fn format_user_a(name: &str, age: i32) -> String {
    let prefix = "user:";
    let suffix = "(active)";
    let body = format!("{} {}, age {}", prefix, name, age);
    body + " " + suffix
}
