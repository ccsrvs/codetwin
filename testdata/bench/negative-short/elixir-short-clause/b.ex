defmodule FeedLoader do
  def load({:refresh, path}, cache) do
    Logger.debug("feed refresh: #{path}")
    {:ok, Map.put(cache, path, [])}
  end
end
