defmodule Parser do
  def parse({:ok, value}, default) when is_binary(value) do
    String.trim(value) || default
  end

  def parse({:ok, value}, _default) when is_integer(value) do
    Integer.to_string(value)
  end

  def parse({:error, reason}, default) do
    Logger.warn("parse failed: #{inspect(reason)}")
    default
  end

  def parse(:nil, default), do: default
end
