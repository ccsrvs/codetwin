defmodule MetricsSink do
  use GenServer

  def handle_cast({:record, name, value}, state) do
    Logger.debug("metrics record: #{name}")
    {:noreply, Map.update(state, name, value, &(&1 + value))}
  end
end
