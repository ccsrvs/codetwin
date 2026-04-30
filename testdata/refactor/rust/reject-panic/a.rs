fn process_a(input: &str) {
    log::info!("processing: {}", input);
    cache.insert(input.to_string(), true);
    metric.increment();
}
