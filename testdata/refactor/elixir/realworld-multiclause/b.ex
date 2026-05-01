defmodule Decoder do
  def decode({:ok, value}, default) when is_binary(value) do
    String.trim(value) || default
  end

  def decode({:ok, value}, _default) when is_integer(value) do
    Integer.to_string(value)
  end

  def decode({:error, reason}, default) do
    Logger.warn("decode failed: #{inspect(reason)}")
    default
  end

  def decode(:nil, default), do: default
end
