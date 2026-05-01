defmodule Trace.A do
  defmacro trace(expr) do
    quote do
      result = unquote(expr)
      Logger.debug("trace_a value=#{inspect(result)}")
      result
    end
  end

  defmacrop sanitize(value) do
    quote do
      unquote(value) |> to_string() |> String.trim()
    end
  end
end
