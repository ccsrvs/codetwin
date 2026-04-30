fn process_b(input: &str) {
    log::info!("processing: {}", input);
    cache.insert(input.to_string(), true);
    panic!("bad state");
}
