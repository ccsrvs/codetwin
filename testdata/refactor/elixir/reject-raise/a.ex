defmodule ProcessorA do
  def process(input) do
    Logger.info("processing: #{input}")
    normalized = String.trim(input)
    Cache.put(normalized, true)
    Metrics.increment("processor.calls", 1)
    :ok
  end
end
