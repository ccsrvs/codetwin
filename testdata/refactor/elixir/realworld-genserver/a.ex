defmodule UserCache do
  use GenServer

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call({:get, key}, _from, state) do
    case Map.fetch(state, key) do
      {:ok, value} -> {:reply, value, state}
      :error -> {:reply, nil, state}
    end
  end

  @impl true
  def handle_cast({:put, key, value}, state) do
    Logger.info("user_cache put: #{key}")
    {:noreply, Map.put(state, key, value)}
  end

  defp lookup(state, key), do: Map.get(state, key)
end
