defmodule Renderer.A do
  @doc """
  Renders a value for display.

  Binaries pass through trimmed; anything else is inspected.
  """
  @spec render(binary() | term()) :: String.t()
  def render(value) when is_binary(value) do
    trimmed = String.trim(value)
    "[" <> trimmed <> "]"
  end

  def render(value) do
    encoded = inspect(value)
    "[" <> encoded <> "]"
  end

  @doc "Builds a cache key for the rendered value."
  @spec cache_key(String.t(), integer()) :: String.t()
  def cache_key(ns, id) do
    prefix = String.trim(ns)
    "#{prefix}:#{id}:v1"
  end
end
