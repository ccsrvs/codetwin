defmodule ProcessorB do
  def process(input) do
    Logger.info("processing: #{input}")
    Cache.put(input, true)
    raise "bad state"
  end
end
