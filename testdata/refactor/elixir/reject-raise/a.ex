defmodule ProcessorA do
  def process(input) do
    Logger.info("processing: #{input}")
    Cache.put(input, true)
    :ok
  end
end
