defmodule MathUtils do
  def sum_list(items) do
    Enum.reduce(items, 0, fn x, acc -> acc + x end)
  end
end
