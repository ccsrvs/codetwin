defmodule Renderer.B do
  @doc """
  Renders a value for output.

  Binaries pass through trimmed; anything else is inspected.
  """
  @spec render(binary() | term()) :: binary()
  def render(value) when is_binary(value) do
    trimmed = String.trim(value)
    "[" <> trimmed <> "]"
  end

  def render(value) when is_atom(value) do
    encoded = Atom.to_string(value)
    "[" <> encoded <> "]"
  end

  def render(value) do
    encoded = inspect(value)
    "[" <> encoded <> "]"
  end

  @doc "Builds a cache key for the rendered value."
  @spec cache_key(String.t(), integer()) :: String.t()
  def cache_key(ns, id) do
    prefix = String.trim(ns)
    "#{prefix}:#{id}:v2"
  end
end
